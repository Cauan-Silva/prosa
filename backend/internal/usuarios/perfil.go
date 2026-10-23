package usuarios

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
)

// Perfil é o que os outros veem de alguém.
type Perfil struct {
	Avatar      string `json:"avatar"` // endereço da imagem ("" = iniciais)
	Banner      string `json:"banner"`
	CorPerfil   int    `json:"corPerfil"` // cor do banner quando não há imagem (0 = automática)
	Bio         string `json:"bio"`
	Pronomes    string `json:"pronomes"`
	Status      string `json:"status"` // online, ausente, ocupado (invisível aparece como offline)
	StatusTexto string `json:"statusTexto"`
	StatusEmoji string `json:"statusEmoji"`
}

// Imagens de perfil ficam em "perfis/<usuario>/<aleatório>" e são servidas sem login em
// /api/perfis/<usuario>/<aleatório>: o nome aleatório já é impossível de adivinhar.
var padraoImagem = regexp.MustCompile(`^perfis/(\d{1,19})/([0-9a-f]{24})$`)

// URLImagem converte a chave guardada no banco no endereço público da imagem.
func URLImagem(chave string) string {
	if chave == "" {
		return ""
	}
	return "/api/" + chave
}

var statusValidos = map[string]bool{"online": true, "ausente": true, "ocupado": true, "invisivel": true}

type pedidoPerfil struct {
	NomeExibicao *string `json:"nomeExibicao"`
	Bio          *string `json:"bio"`
	Pronomes     *string `json:"pronomes"`
	CorPerfil    *int    `json:"corPerfil"`
	Status       *string `json:"status"`
	StatusTexto  *string `json:"statusTexto"`
	StatusEmoji  *string `json:"statusEmoji"`
}

func limpar(texto string, max int, campo string) (string, error) {
	texto = strings.TrimSpace(texto)
	if utf8.RuneCountInString(texto) > max {
		return "", api.NovoErro(http.StatusBadRequest, campo+" pode ter até "+strconv.Itoa(max)+" caracteres.")
	}
	return texto, nil
}

// AlterarPerfil: PATCH /api/eu — muda só os campos enviados.
func (s *Servico) AlterarPerfil(w http.ResponseWriter, r *http.Request) error {
	eu := api.Quem(r).UsuarioID
	var p pedidoPerfil
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	antes, err := s.Buscar(r.Context(), eu)
	if err != nil {
		return err
	}
	novo := antes
	if p.NomeExibicao != nil {
		nome := strings.Join(strings.Fields(*p.NomeExibicao), " ")
		if n := utf8.RuneCountInString(nome); n < 1 || n > 40 {
			return api.NovoErro(http.StatusBadRequest, "O nome de exibição precisa ter de 1 a 40 caracteres.")
		}
		novo.NomeExibicao = nome
	}
	if p.Bio != nil {
		if novo.Bio, err = limpar(*p.Bio, 400, "O \"sobre mim\""); err != nil {
			return err
		}
	}
	if p.Pronomes != nil {
		if novo.Pronomes, err = limpar(*p.Pronomes, 40, "Os pronomes"); err != nil {
			return err
		}
	}
	if p.StatusTexto != nil {
		if novo.StatusTexto, err = limpar(*p.StatusTexto, 80, "O status"); err != nil {
			return err
		}
	}
	if p.StatusEmoji != nil {
		e := strings.TrimSpace(*p.StatusEmoji)
		if utf8.RuneCountInString(e) > 8 || len(e) > 16 {
			return api.NovoErro(http.StatusBadRequest, "Use um emoji só no status.")
		}
		novo.StatusEmoji = e
	}
	if p.CorPerfil != nil {
		if *p.CorPerfil < 0 || *p.CorPerfil > 0xffffff {
			return api.NovoErro(http.StatusBadRequest, "Cor inválida.")
		}
		novo.CorPerfil = *p.CorPerfil
	}
	if p.Status != nil {
		if !statusValidos[*p.Status] {
			return api.NovoErro(http.StatusBadRequest, "Status inválido.")
		}
		novo.Status = *p.Status
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE usuarios SET nome_exibicao = $2, bio = $3, pronomes = $4, cor_perfil = $5, status = $6,
		       status_texto = $7, status_emoji = $8
		WHERE id = $1`, eu, novo.NomeExibicao, novo.Bio, novo.Pronomes, novo.CorPerfil, novo.Status,
		novo.StatusTexto, novo.StatusEmoji); err != nil {
		return err
	}
	if (antes.Status == "invisivel") != (novo.Status == "invisivel") && s.AoMudarStatus != nil {
		s.AoMudarStatus(r.Context(), eu, novo.Status == "invisivel")
	}
	return s.responderEAvisar(w, r)
}

// responderEAvisar devolve a conta e avisa os servidores da pessoa que o perfil mudou.
func (s *Servico) responderEAvisar(w http.ResponseWriter, r *http.Request) error {
	u, err := s.Buscar(r.Context(), api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	s.avisar(r.Context(), u)
	api.Responder(w, http.StatusOK, u)
	return nil
}

// PerfilPublico é o que vai nos eventos e na lista de membros (sem e-mail e preferências).
type PerfilPublico struct {
	UsuarioID    int64  `json:"usuarioId"`
	NomeUsuario  string `json:"nomeUsuario"`
	NomeExibicao string `json:"nomeExibicao"`
	Perfil
}

func publico(u Usuario) PerfilPublico {
	p := PerfilPublico{UsuarioID: u.ID, NomeUsuario: u.NomeUsuario, NomeExibicao: u.NomeExibicao, Perfil: u.Perfil}
	if p.Status == "invisivel" {
		p.Status = "online" // para os outros, invisível é só "não está conectado"
	}
	return p
}

func (s *Servico) avisar(ctx context.Context, u Usuario) {
	if s.pub == nil {
		return
	}
	linhas, err := s.db.QueryContext(ctx, `SELECT servidor_id FROM membros WHERE usuario_id = $1`, u.ID)
	if err != nil {
		return
	}
	defer linhas.Close()
	dados := publico(u)
	for linhas.Next() {
		var sid int64
		if linhas.Scan(&sid) != nil {
			continue
		}
		e := eventos.Novo(eventos.UsuarioAlterado, sid, 0, dados)
		e.UsuarioID = u.ID
		if err := s.pub.Publicar(ctx, eventos.TopicoServidor(sid), e); err != nil {
			slog.Warn("perfil: publicar", "erro", err)
		}
	}
	// as outras abas da própria pessoa, os amigos e quem conversa com ela em DM ou grupo
	e := eventos.Novo(eventos.UsuarioAlterado, 0, 0, dados)
	e.UsuarioID = u.ID
	var outros pq.Int64Array
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(array_agg(DISTINCT id), '{}') FROM (
			SELECT CASE WHEN de_id = $1 THEN para_id ELSE de_id END AS id FROM amizades WHERE estado = 'aceita' AND (de_id = $1 OR para_id = $1)
			UNION SELECT b.usuario_id FROM conversa_membros a JOIN conversa_membros b ON b.canal_id = a.canal_id WHERE a.usuario_id = $1 AND b.usuario_id <> $1
		) t`, u.ID).Scan(&outros)
	for _, id := range append([]int64{u.ID}, outros...) {
		_ = s.pub.Publicar(ctx, eventos.TopicoUsuario(id), e)
	}
}

// VerPerfil: GET /api/usuarios/{usuario} — de quem divide servidor ou conversa com você,
// é seu amigo ou tem um pedido de amizade com você. Vem junto a relação entre vocês.
func (s *Servico) VerPerfil(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("usuario"), 10, 64)
	naoAchou := api.NovoErro(http.StatusNotFound, "Pessoa não encontrada.")
	if err != nil {
		return naoAchou
	}
	eu := api.Quem(r).UsuarioID
	var servidores pq.Int64Array
	var relacao string
	var conversa, bloqueei bool
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT (SELECT COALESCE(array_agg(a.servidor_id), '{}') FROM membros a
		        JOIN membros b ON b.servidor_id = a.servidor_id AND b.usuario_id = $2 WHERE a.usuario_id = $1),
		       COALESCE((SELECT CASE WHEN estado = 'aceita' THEN 'amigo' WHEN de_id = $2 THEN 'pedido_enviado' ELSE 'pedido_recebido' END
		                 FROM amizades WHERE (de_id = $1 AND para_id = $2) OR (de_id = $2 AND para_id = $1)), ''),
		       EXISTS (SELECT 1 FROM conversa_membros a JOIN conversa_membros b ON b.canal_id = a.canal_id
		               WHERE a.usuario_id = $1 AND b.usuario_id = $2),
		       EXISTS (SELECT 1 FROM bloqueios WHERE quem_id = $2 AND bloqueado_id = $1)`, id, eu).
		Scan(&servidores, &relacao, &conversa, &bloqueei); err != nil {
		return err
	}
	if bloqueei {
		relacao = "bloqueado"
	}
	if id != eu && len(servidores) == 0 && relacao == "" && !conversa {
		return naoAchou
	}
	u, err := s.Buscar(r.Context(), id)
	if err != nil {
		return naoAchou
	}
	api.Responder(w, http.StatusOK, struct {
		PerfilPublico
		CriadoEm          time.Time `json:"criadoEm"`
		ServidoresEmComum []int64   `json:"servidoresEmComum"`
		Relacao           string    `json:"relacao"` // amigo, pedido_enviado, pedido_recebido, bloqueado ou ""
	}{publico(u), u.CriadoEm, servidores, relacao})
	return nil
}

// SalvarPreferencias: PUT /api/eu/preferencias — tema, cores, voz… (JSON livre, até 16 KB)
// para a pessoa ter as mesmas configurações em qualquer navegador.
func (s *Servico) SalvarPreferencias(w http.ResponseWriter, r *http.Request) error {
	corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		return api.NovoErro(http.StatusRequestEntityTooLarge, "Preferências grandes demais.")
	}
	var objeto map[string]any
	if json.Unmarshal(corpo, &objeto) != nil || objeto == nil {
		return api.NovoErro(http.StatusBadRequest, "Preferências inválidas.")
	}
	limpo, _ := json.Marshal(objeto)
	if _, err := s.db.ExecContext(r.Context(), `UPDATE usuarios SET preferencias = $2 WHERE id = $1`,
		api.Quem(r).UsuarioID, limpo); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- imagens de perfil ----

var limitesImagem = map[string]int64{"avatar": 4 << 20, "banner": 8 << 20}

var tiposImagem = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// EnviarImagem: POST /api/eu/avatar ou /api/eu/banner (multipart, campo "arquivo").
func (s *Servico) EnviarImagem(campo string) func(w http.ResponseWriter, r *http.Request) error {
	limite := limitesImagem[campo]
	return func(w http.ResponseWriter, r *http.Request) error {
		if s.armazem == nil {
			return api.NovoErro(http.StatusServiceUnavailable, "Envio de imagens desativado.")
		}
		eu := api.Quem(r).UsuarioID
		r.Body = http.MaxBytesReader(w, r.Body, limite+64<<10)
		arquivo, cabecalho, err := r.FormFile("arquivo")
		if err != nil || cabecalho.Size > limite {
			return api.NovoErro(http.StatusBadRequest, "Mande uma imagem de até "+strconv.FormatInt(limite>>20, 10)+" MB.")
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
		chave := "perfis/" + strconv.FormatInt(eu, 10) + "/" + hex.EncodeToString(aleatorio)
		if err := s.armazem.Gravar(r.Context(), chave, bytes.NewReader(dados), int64(len(dados)), tipo); err != nil {
			slog.Error("perfil: guardar imagem", "erro", err)
			return api.NovoErro(http.StatusBadGateway, "Não foi possível guardar a imagem agora.")
		}
		return s.trocarImagem(w, r, campo, chave)
	}
}

// ApagarImagem: DELETE /api/eu/avatar ou /api/eu/banner.
func (s *Servico) ApagarImagem(campo string) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error { return s.trocarImagem(w, r, campo, "") }
}

func (s *Servico) trocarImagem(w http.ResponseWriter, r *http.Request, campo, chave string) error {
	eu := api.Quem(r).UsuarioID
	var antiga string
	// campo vem só de "avatar"/"banner" (fixos nas rotas), nunca do pedido
	if err := s.db.QueryRowContext(r.Context(),
		`UPDATE usuarios u SET `+campo+` = $2 FROM (SELECT `+campo+` AS antiga FROM usuarios WHERE id = $1) a
		 WHERE u.id = $1 RETURNING a.antiga`, eu, chave).Scan(&antiga); err != nil {
		return err
	}
	if antiga != "" && s.armazem != nil {
		if err := s.armazem.Apagar(r.Context(), antiga); err != nil {
			slog.Warn("perfil: imagem antiga ficou no armazenamento", "chave", antiga, "erro", err)
		}
	}
	return s.responderEAvisar(w, r)
}

// Imagem: GET /api/perfis/{usuario}/{arquivo} — pública (o nome é impossível de adivinhar).
func (s *Servico) Imagem(w http.ResponseWriter, r *http.Request) {
	chave := "perfis/" + r.PathValue("usuario") + "/" + r.PathValue("arquivo")
	if !padraoImagem.MatchString(chave) || s.armazem == nil {
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

// Invisiveis diz quem da lista está invisível (o gateway não mostra essas pessoas online).
func (s *Servico) Invisiveis(ctx context.Context, ids []int64) (map[int64]bool, error) {
	saida := map[int64]bool{}
	if len(ids) == 0 {
		return saida, nil
	}
	linhas, err := s.db.QueryContext(ctx, `SELECT id FROM usuarios WHERE id = ANY($1) AND status = 'invisivel'`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	for linhas.Next() {
		var id int64
		if linhas.Scan(&id) == nil {
			saida[id] = true
		}
	}
	return saida, linhas.Err()
}
