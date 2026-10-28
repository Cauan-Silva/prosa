// Package servidores cuida dos servidores, canais, categorias, cargos, membros, convites,
// moderação e do registro de auditoria.
package servidores

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/usuarios"
)

type Servico struct {
	db      *sql.DB
	pub     eventos.Publicador
	armazem arquivos.Armazem // ícones dos servidores (nil = envio desativado)
	agora   func() time.Time
}

func NovoServico(db *sql.DB, pub eventos.Publicador, armazem arquivos.Armazem) *Servico {
	return &Servico{db: db, pub: pub, armazem: armazem, agora: time.Now}
}

// ---- modelos ----

type Servidor struct {
	ID       int64     `json:"id"`
	Nome     string    `json:"nome"`
	DonoID   int64     `json:"donoId"`
	CriadoEm time.Time `json:"criadoEm"`
	Icone    string    `json:"icone"` // endereço da imagem ("" = azulejo)
}

type Categoria struct {
	ID         int64  `json:"id"`
	ServidorID int64  `json:"servidorId"`
	Nome       string `json:"nome"`
	Posicao    int    `json:"posicao"`
}

type Canal struct {
	ID          int64  `json:"id"`
	ServidorID  int64  `json:"servidorId"`
	CategoriaID *int64 `json:"categoriaId"`
	Nome        string `json:"nome"`
	Tipo        string `json:"tipo"`
	Topico      string `json:"topico"`
	Posicao     int    `json:"posicao"`
	ModoLento   int    `json:"modoLento"`
	// Só na visão de uma pessoa:
	Permissoes  uint64 `json:"permissoes,omitempty"`
	UltimaMsgID int64  `json:"ultimaMensagemId,omitempty"`
	UltimaLida  int64  `json:"ultimaLida,omitempty"`
}

type Cargo struct {
	ID          int64  `json:"id"`
	ServidorID  int64  `json:"servidorId"`
	Nome        string `json:"nome"`
	Cor         int    `json:"cor"`
	Permissoes  uint64 `json:"permissoes"`
	Posicao     int    `json:"posicao"`
	Padrao      bool   `json:"padrao"`
	Mencionavel bool   `json:"mencionavel"`
}

type Membro struct {
	UsuarioID    int64      `json:"usuarioId"`
	NomeUsuario  string     `json:"nomeUsuario"`
	NomeExibicao string     `json:"nomeExibicao"`
	Apelido      *string    `json:"apelido"`
	Cargos       []int64    `json:"cargos"`
	CastigadoAte *time.Time `json:"castigadoAte,omitempty"`
	EntrouEm     time.Time  `json:"entrouEm"`
	// perfil (o resto — bio, banner, pronomes — vem em GET /api/usuarios/{id})
	Avatar      string          `json:"avatar"`
	CorPerfil   int             `json:"corPerfil"`
	Status      string          `json:"status"`
	StatusTexto string          `json:"statusTexto"`
	StatusEmoji string          `json:"statusEmoji"`
	Visual      usuarios.Visual `json:"visual"`
}

// ---- utilidades ----

func id(r *http.Request, nome string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(nome), 10, 64)
	if err != nil || v <= 0 {
		return 0, api.NovoErro(http.StatusNotFound, "Não encontrado.")
	}
	return v, nil
}

// contexto carrega as permissões de quem fez o pedido no servidor da rota.
func (s *Servico) contexto(r *http.Request) (*acesso.Contexto, int64, error) {
	servidorID, err := id(r, "servidor")
	if err != nil {
		return nil, 0, err
	}
	c, err := acesso.Carregar(r.Context(), s.db, servidorID, api.Quem(r).UsuarioID)
	return c, servidorID, err
}

// publicar avisa as conexões. Uma falha aqui não desfaz o que já foi gravado: só fica no log.
func (s *Servico) publicar(ctx context.Context, e eventos.Evento) {
	topico := eventos.TopicoServidor(e.ServidorID)
	if err := s.pub.Publicar(ctx, topico, e); err != nil {
		slog.Error("não foi possível publicar o evento", "tipo", e.Tipo, "erro", err)
	}
}

func (s *Servico) publicarUsuario(ctx context.Context, usuarioID int64, e eventos.Evento) {
	if err := s.pub.Publicar(ctx, eventos.TopicoUsuario(usuarioID), e); err != nil {
		slog.Error("não foi possível publicar o evento", "tipo", e.Tipo, "erro", err)
	}
}

// auditar registra uma ação de moderação ou de configuração.
func (s *Servico) auditar(ctx context.Context, servidorID, autorID int64, acao, alvo, detalhes string) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO auditoria (servidor_id, autor_id, acao, alvo, detalhes) VALUES ($1, $2, $3, $4, $5)`,
		servidorID, autorID, acao, limitar(alvo, 120), limitar(detalhes, 500)); err != nil {
		slog.Error("não foi possível registrar a auditoria", "acao", acao, "erro", err)
	}
}

func limitar(texto string, max int) string {
	r := []rune(texto)
	if len(r) > max {
		return string(r[:max])
	}
	return texto
}

// emTransacao roda fn numa transação e faz commit se não houver erro.
func (s *Servico) emTransacao(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// MembrosDe lista os servidores de que a pessoa faz parte (o tempo real assina cada um).
func (s *Servico) ServidoresDe(ctx context.Context, usuarioID int64) ([]int64, error) {
	linhas, err := s.db.QueryContext(ctx, `SELECT servidor_id FROM membros WHERE usuario_id = $1`, usuarioID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var ids []int64
	for linhas.Next() {
		var v int64
		if err := linhas.Scan(&v); err != nil {
			return nil, err
		}
		ids = append(ids, v)
	}
	return ids, linhas.Err()
}

func itoa(n int) string     { return strconv.Itoa(n) }
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
