package mensagens

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

func enderecoAnexo(id int64, nome string) string {
	return "/api/anexos/" + strconv.FormatInt(id, 10) + "/" + escaparNome(nome)
}

func escaparNome(nome string) string {
	return strings.ReplaceAll(strings.ReplaceAll(nome, "/", "_"), "?", "_")
}

// Tipos que o navegador pode mostrar direto na página; o resto é baixado como arquivo.
var tiposSeguros = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true,
	"application/pdf": true, "text/plain": true,
}

// EnviarAnexo: POST /api/canais/{canal}/anexos (multipart, campo "arquivo").
// Devolve o anexo, que depois vai junto da mensagem (campo "anexos").
func (s *Servico) EnviarAnexo(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.AnexarArquivos); err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxAnexo+64<<10)
	arquivo, cabecalho, err := r.FormFile("arquivo")
	if err != nil {
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			return api.NovoErro(http.StatusRequestEntityTooLarge, "O arquivo passou do limite de "+strconv.FormatInt(s.maxAnexo>>20, 10)+" MB.")
		}
		return api.NovoErro(http.StatusBadRequest, "Mande o arquivo no campo \"arquivo\".")
	}
	defer arquivo.Close()
	if cabecalho.Size > s.maxAnexo {
		return api.NovoErro(http.StatusRequestEntityTooLarge, "O arquivo passou do limite de "+strconv.FormatInt(s.maxAnexo>>20, 10)+" MB.")
	}

	nome := path.Base(strings.ReplaceAll(cabecalho.Filename, "\\", "/"))
	if nome == "" || nome == "." || nome == "/" {
		nome = "arquivo"
	}
	if utf8.RuneCountInString(nome) > 200 {
		nome = string([]rune(nome)[:200])
	}

	// O tipo vem do conteúdo, não do que o navegador diz (evita HTML disfarçado de imagem).
	cabeca := make([]byte, 512)
	n, _ := io.ReadFull(arquivo, cabeca)
	tipo := http.DetectContentType(cabeca[:n])
	if base, _, err := mime.ParseMediaType(tipo); err == nil {
		tipo = base
	}
	if !tiposSeguros[tipo] {
		tipo = "application/octet-stream"
	}
	if _, err := arquivo.Seek(0, io.SeekStart); err != nil {
		return err
	}

	aleatorio := make([]byte, 12)
	_, _ = rand.Read(aleatorio)
	chave := "anexos/" + strconv.FormatInt(c.ServidorID, 10) + "/" + hex.EncodeToString(aleatorio)
	if err := s.armazem.Gravar(r.Context(), chave, arquivo, cabecalho.Size, tipo); err != nil {
		slog.Error("não foi possível guardar o anexo", "erro", err)
		return api.NovoErro(http.StatusBadGateway, "Não foi possível guardar o arquivo agora. Tente de novo.")
	}
	a := Anexo{Nome: nome, Tipo: tipo, Tamanho: cabecalho.Size}
	if err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO anexos (enviado_por, servidor_id, nome, tipo, tamanho, chave) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		c.Membro.UsuarioID, c.ServidorID, nome, tipo, cabecalho.Size, chave).Scan(&a.ID); err != nil {
		return err
	}
	a.Endereco = enderecoAnexo(a.ID, nome)
	api.Responder(w, http.StatusCreated, a)
	return nil
}

// BaixarAnexo: GET /api/anexos/{id}/{nome}. Confere se a pessoa vê o canal da mensagem e
// redireciona para um link temporário do S3 (ou entrega o arquivo da pasta local).
func (s *Servico) BaixarAnexo(w http.ResponseWriter, r *http.Request) error {
	id, err := idDaRota(r, "anexo")
	if err != nil {
		return err
	}
	var nome, tipo, chave string
	var servidorID, enviadoPor int64
	var canalID sql.NullInt64
	err = s.db.QueryRowContext(r.Context(), `
		SELECT a.nome, a.tipo, a.chave, a.servidor_id, a.enviado_por, m.canal_id
		FROM anexos a LEFT JOIN mensagens m ON m.id = a.mensagem_id WHERE a.id = $1`, id).
		Scan(&nome, &tipo, &chave, &servidorID, &enviadoPor, &canalID)
	naoAchou := api.NovoErro(http.StatusNotFound, "Arquivo não encontrado.")
	if errors.Is(err, sql.ErrNoRows) {
		return naoAchou
	}
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	if canalID.Valid {
		c, err := acesso.Carregar(r.Context(), s.db, servidorID, eu)
		if err != nil || !c.NoCanal(canalID.Int64).Tem(permissoes.LerHistorico) {
			return naoAchou
		}
	} else if enviadoPor != eu { // ainda não foi para nenhuma mensagem: só quem enviou vê
		return naoAchou
	}

	if endereco, ok := s.armazem.Endereco(chave, nome, 10*time.Minute); ok {
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.Redirect(w, r, endereco, http.StatusFound)
		return nil
	}
	f, err := s.armazem.Abrir(r.Context(), chave)
	if err != nil {
		return naoAchou
	}
	defer f.Close()
	disposicao := "attachment"
	if tipo != "application/octet-stream" {
		disposicao = "inline"
	}
	w.Header().Set("Content-Type", tipo)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposicao, map[string]string{"filename": nome}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, err = io.Copy(w, f)
	return err
}
