package servidores

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Ícones ficam em "icones/<servidor>/<aleatório>" e são servidos sem login em
// /api/icones/<servidor>/<aleatório> (aparecem na página de convite, antes de entrar).
var padraoIcone = regexp.MustCompile(`^icones/(\d{1,19})/([0-9a-f]{24})$`)

const limiteIcone = 4 << 20

var tiposImagem = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// urlIcone converte a chave guardada no banco no endereço público da imagem.
func urlIcone(chave string) string {
	if chave == "" {
		return ""
	}
	return "/api/" + chave
}

const colunasServidor = `s.id, s.nome, s.dono_id, s.criado_em, s.icone`

func lerServidor(l interface{ Scan(...any) error }) (Servidor, error) {
	var v Servidor
	err := l.Scan(&v.ID, &v.Nome, &v.DonoID, &v.CriadoEm, &v.Icone)
	v.Icone = urlIcone(v.Icone)
	return v, err
}

// EnviarIcone: POST /api/servidores/{servidor}/icone (multipart, campo "arquivo").
func (s *Servico) EnviarIcone(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	if s.armazem == nil {
		return api.NovoErro(http.StatusServiceUnavailable, "Envio de imagens desativado.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limiteIcone+64<<10)
	arquivo, cabecalho, err := r.FormFile("arquivo")
	if err != nil || cabecalho.Size > limiteIcone {
		return api.NovoErro(http.StatusBadRequest, "Mande uma imagem de até 4 MB.")
	}
	defer arquivo.Close()
	dados, err := io.ReadAll(arquivo)
	if err != nil {
		return err
	}
	tipo := http.DetectContentType(dados)
	if !tiposImagem[tipo] {
		return api.NovoErro(http.StatusBadRequest, "Use uma imagem PNG, JPG, GIF ou WebP.")
	}
	aleatorio := make([]byte, 12)
	_, _ = rand.Read(aleatorio)
	chave := "icones/" + strconv.FormatInt(servidorID, 10) + "/" + hex.EncodeToString(aleatorio)
	if err := s.armazem.Gravar(r.Context(), chave, bytes.NewReader(dados), int64(len(dados)), tipo); err != nil {
		slog.Error("servidor: guardar ícone", "erro", err)
		return api.NovoErro(http.StatusBadGateway, "Não foi possível guardar a imagem agora.")
	}
	return s.trocarIcone(w, r, servidorID, c.Membro.UsuarioID, chave)
}

// ApagarIcone: DELETE /api/servidores/{servidor}/icone (volta para o azulejo).
func (s *Servico) ApagarIcone(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	return s.trocarIcone(w, r, servidorID, c.Membro.UsuarioID, "")
}

func (s *Servico) trocarIcone(w http.ResponseWriter, r *http.Request, servidorID, autorID int64, chave string) error {
	var antiga string
	var v Servidor
	if err := s.db.QueryRowContext(r.Context(), `
		UPDATE servidores s SET icone = $2 FROM (SELECT icone AS antiga FROM servidores WHERE id = $1) a
		WHERE s.id = $1 RETURNING `+colunasServidor+`, a.antiga`, servidorID, chave).
		Scan(&v.ID, &v.Nome, &v.DonoID, &v.CriadoEm, &v.Icone, &antiga); err != nil {
		return err
	}
	v.Icone = urlIcone(v.Icone)
	if antiga != "" && s.armazem != nil {
		if err := s.armazem.Apagar(r.Context(), antiga); err != nil {
			slog.Warn("servidor: ícone antigo ficou no armazenamento", "chave", antiga, "erro", err)
		}
	}
	acao := "icone_alterado"
	if chave == "" {
		acao = "icone_removido"
	}
	s.auditar(r.Context(), servidorID, autorID, acao, v.Nome, "")
	s.publicar(r.Context(), eventos.Novo(eventos.ServidorAlterado, servidorID, 0, v))
	api.Responder(w, http.StatusOK, v)
	return nil
}

// Icone: GET /api/icones/{servidor}/{arquivo} — público (o nome é impossível de adivinhar).
func (s *Servico) Icone(w http.ResponseWriter, r *http.Request) {
	chave := "icones/" + r.PathValue("servidor") + "/" + r.PathValue("arquivo")
	if !padraoIcone.MatchString(chave) || s.armazem == nil {
		http.NotFound(w, r)
		return
	}
	if endereco, ok := s.armazem.Endereco(chave, "", time.Hour); ok {
		w.Header().Set("Cache-Control", "public, max-age=3000")
		http.Redirect(w, r, endereco, http.StatusFound)
		return
	}
	f, err := s.armazem.Abrir(r.Context(), chave)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	cabeca := make([]byte, 512)
	n, _ := io.ReadFull(f, cabeca)
	tipo := http.DetectContentType(cabeca[:n])
	if !tiposImagem[tipo] {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", tipo)
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(cabeca[:n])
	_, _ = io.Copy(w, f)
}
