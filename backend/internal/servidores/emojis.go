package servidores

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Emojis do servidor: quem gerencia o servidor envia, todo mundo usa com :nome:. Nas
// mensagens ficam como <:nome:id>, então funcionam em qualquer canal ou DM (a imagem é
// pública em /api/emojis/{id}).

const (
	maxEmojis     = 50
	limiteEmoji   = 512 << 10
	EmojisMudaram = "EMOJIS_ALTERADOS"
)

var nomeEmojiValido = regexp.MustCompile(`^[A-Za-z0-9_]{2,32}$`)

type Emoji struct {
	ID      int64  `json:"id"`
	Nome    string `json:"nome"`
	Animado bool   `json:"animado"`
	URL     string `json:"url"`
}

func urlEmoji(id int64) string { return "/api/emojis/" + strconv.FormatInt(id, 10) }

func (s *Servico) emojis(ctx context.Context, servidorID int64) ([]Emoji, error) {
	linhas, err := s.db.QueryContext(ctx, `SELECT id, nome, animado FROM emojis WHERE servidor_id = $1 ORDER BY lower(nome)`, servidorID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []Emoji{}
	for linhas.Next() {
		var e Emoji
		if err := linhas.Scan(&e.ID, &e.Nome, &e.Animado); err != nil {
			return nil, err
		}
		e.URL = urlEmoji(e.ID)
		lista = append(lista, e)
	}
	return lista, linhas.Err()
}

func (s *Servico) avisarEmojis(r *http.Request, servidorID int64) {
	s.publicar(r.Context(), eventos.Novo(EmojisMudaram, servidorID, 0, nil))
}

// CriarEmoji: POST /api/servidores/{servidor}/emojis (multipart: "nome" e "arquivo").
func (s *Servico) CriarEmoji(w http.ResponseWriter, r *http.Request) error {
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
	r.Body = http.MaxBytesReader(w, r.Body, limiteEmoji+64<<10)
	nome := strings.Trim(strings.TrimSpace(r.FormValue("nome")), ":")
	if !nomeEmojiValido.MatchString(nome) {
		return api.NovoErro(http.StatusBadRequest, "O nome do emoji precisa ter de 2 a 32 letras, números ou _ (sem espaço nem acento).")
	}
	arquivo, cabecalho, err := r.FormFile("arquivo")
	if err != nil || cabecalho.Size > limiteEmoji {
		return api.NovoErro(http.StatusBadRequest, "Mande uma imagem de até 512 KB.")
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
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM emojis WHERE servidor_id = $1`, servidorID).Scan(&total); err != nil {
		return err
	}
	if total >= maxEmojis {
		return api.NovoErro(http.StatusBadRequest, "Este servidor já tem 50 emojis. Apague algum antes.")
	}
	aleatorio := make([]byte, 12)
	_, _ = rand.Read(aleatorio)
	chave := "emojis/" + strconv.FormatInt(servidorID, 10) + "/" + hex.EncodeToString(aleatorio)
	if err := s.armazem.Gravar(r.Context(), chave, bytes.NewReader(dados), int64(len(dados)), tipo); err != nil {
		slog.Error("emoji: guardar", "erro", err)
		return api.NovoErro(http.StatusBadGateway, "Não foi possível guardar a imagem agora.")
	}
	e := Emoji{Nome: nome, Animado: tipo == "image/gif" || (tipo == "image/webp" && bytes.Contains(dados[:min(len(dados), 64<<10)], []byte("ANIM")))}
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO emojis (servidor_id, nome, chave, animado, criado_por) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		servidorID, nome, chave, e.Animado, c.Membro.UsuarioID).Scan(&e.ID)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		_ = s.armazem.Apagar(r.Context(), chave)
		return api.NovoErro(http.StatusConflict, "Já existe um emoji :"+nome+": neste servidor.")
	}
	if err != nil {
		return err
	}
	e.URL = urlEmoji(e.ID)
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "emoji_criado", ":"+nome+":", "")
	s.avisarEmojis(r, servidorID)
	api.Responder(w, http.StatusCreated, e)
	return nil
}

// AlterarEmoji: PATCH /api/servidores/{servidor}/emojis/{emoji} {"nome": "..."}
func (s *Servico) AlterarEmoji(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	emojiID, err := id(r, "emoji")
	if err != nil {
		return err
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := strings.Trim(strings.TrimSpace(p.Nome), ":")
	if !nomeEmojiValido.MatchString(nome) {
		return api.NovoErro(http.StatusBadRequest, "O nome do emoji precisa ter de 2 a 32 letras, números ou _ (sem espaço nem acento).")
	}
	var e Emoji
	err = s.db.QueryRowContext(r.Context(), `UPDATE emojis SET nome = $3 WHERE id = $1 AND servidor_id = $2 RETURNING id, nome, animado`,
		emojiID, servidorID, nome).Scan(&e.ID, &e.Nome, &e.Animado)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return api.NovoErro(http.StatusConflict, "Já existe um emoji :"+nome+": neste servidor.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return api.NovoErro(http.StatusNotFound, "Emoji não encontrado.")
	}
	if err != nil {
		return err
	}
	e.URL = urlEmoji(e.ID)
	s.avisarEmojis(r, servidorID)
	api.Responder(w, http.StatusOK, e)
	return nil
}

// ApagarEmoji: DELETE /api/servidores/{servidor}/emojis/{emoji}
func (s *Servico) ApagarEmoji(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	emojiID, err := id(r, "emoji")
	if err != nil {
		return err
	}
	var chave, nome string
	err = s.db.QueryRowContext(r.Context(), `DELETE FROM emojis WHERE id = $1 AND servidor_id = $2 RETURNING chave, nome`, emojiID, servidorID).Scan(&chave, &nome)
	if errors.Is(err, sql.ErrNoRows) {
		return api.NovoErro(http.StatusNotFound, "Emoji não encontrado.")
	}
	if err != nil {
		return err
	}
	if err := s.armazem.Apagar(r.Context(), chave); err != nil {
		slog.Warn("emoji: imagem ficou no armazenamento", "chave", chave, "erro", err)
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "emoji_apagado", ":"+nome+":", "")
	s.avisarEmojis(r, servidorID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ImagemEmoji: GET /api/emojis/{emoji} — pública (aparece em qualquer mensagem que use o emoji).
func (s *Servico) ImagemEmoji(w http.ResponseWriter, r *http.Request) {
	emojiID, err := strconv.ParseInt(r.PathValue("emoji"), 10, 64)
	if err != nil || s.armazem == nil {
		http.NotFound(w, r)
		return
	}
	var chave string
	if err := s.db.QueryRowContext(r.Context(), `SELECT chave FROM emojis WHERE id = $1`, emojiID).Scan(&chave); err != nil {
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(cabeca[:n])
	_, _ = io.Copy(w, f)
}

// EmojiExiste confere se um emoji (para reação) existe; usado pelo pacote de mensagens.
func EmojiExiste(r *http.Request, db *sql.DB, emojiID int64) bool {
	var ok bool
	_ = db.QueryRowContext(r.Context(), `SELECT EXISTS (SELECT 1 FROM emojis WHERE id = $1)`, emojiID).Scan(&ok)
	return ok
}
