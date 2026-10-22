package servidores

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const consultaMembros = `
	SELECT u.id, u.nome_usuario, u.nome_exibicao, m.apelido, m.castigado_ate, m.entrou_em,
	       COALESCE(array_agg(mc.cargo_id) FILTER (WHERE mc.cargo_id IS NOT NULL), '{}')
	FROM membros m
	JOIN usuarios u ON u.id = m.usuario_id
	LEFT JOIN membros_cargos mc ON mc.servidor_id = m.servidor_id AND mc.usuario_id = m.usuario_id
	WHERE m.servidor_id = $1 %s
	GROUP BY u.id, u.nome_usuario, u.nome_exibicao, m.apelido, m.castigado_ate, m.entrou_em`

func lerMembro(l interface{ Scan(...any) error }, agora time.Time) (Membro, error) {
	var m Membro
	var apelido sql.NullString
	var castigo sql.NullTime
	var cargos pq.Int64Array
	err := l.Scan(&m.UsuarioID, &m.NomeUsuario, &m.NomeExibicao, &apelido, &castigo, &m.EntrouEm, &cargos)
	if apelido.Valid {
		m.Apelido = &apelido.String
	}
	if castigo.Valid && castigo.Time.After(agora) {
		m.CastigadoAte = &castigo.Time
	}
	m.Cargos = cargos
	if m.Cargos == nil {
		m.Cargos = []int64{}
	}
	return m, err
}

func (s *Servico) membro(ctx context.Context, servidorID, usuarioID int64) (Membro, error) {
	m, err := lerMembro(s.db.QueryRowContext(ctx,
		strings.Replace(consultaMembros, "%s", "AND m.usuario_id = $2", 1), servidorID, usuarioID), s.agora())
	if errors.Is(err, sql.ErrNoRows) {
		return m, api.NovoErro(http.StatusNotFound, "Membro não encontrado.")
	}
	return m, err
}

// ListarMembros devolve até 1000 membros (o suficiente para a lista lateral).
func (s *Servico) ListarMembros(w http.ResponseWriter, r *http.Request) error {
	_, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	linhas, err := s.db.QueryContext(r.Context(),
		strings.Replace(consultaMembros, "%s", "", 1)+` ORDER BY lower(COALESCE(m.apelido, u.nome_exibicao)) LIMIT 1000`, servidorID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lista := []Membro{}
	for linhas.Next() {
		m, err := lerMembro(linhas, s.agora())
		if err != nil {
			return err
		}
		lista = append(lista, m)
	}
	if err := linhas.Err(); err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// publicarMembro avisa que o membro mudou (cargos, apelido, castigo) e responde com ele.
func (s *Servico) publicarMembro(w http.ResponseWriter, r *http.Request, servidorID, usuarioID int64) error {
	m, err := s.membro(r.Context(), servidorID, usuarioID)
	if err != nil {
		return err
	}
	e := eventos.Novo(eventos.MembroAlterado, servidorID, 0, m)
	e.UsuarioID = usuarioID
	s.publicar(r.Context(), e)
	api.Responder(w, http.StatusOK, m)
	return nil
}

// Apelido: PUT /api/servidores/{servidor}/membros/{usuario}/apelido
func (s *Servico) Apelido(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	if usuarioID == c.Membro.UsuarioID {
		if err := c.Exigir(permissoes.AlterarApelido); err != nil {
			return err
		}
	} else {
		if err := c.Exigir(permissoes.GerenciarApelidos); err != nil {
			return err
		}
		if err := s.exigirAcima(r, c, servidorID, usuarioID); err != nil {
			return err
		}
	}
	var p struct {
		Apelido string `json:"apelido"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	apelido := strings.Join(strings.Fields(p.Apelido), " ")
	if utf8.RuneCountInString(apelido) > 40 {
		return api.NovoErro(http.StatusBadRequest, "O apelido pode ter até 40 caracteres.")
	}
	var valor any = apelido
	if apelido == "" {
		valor = nil
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE membros SET apelido = $3 WHERE servidor_id = $1 AND usuario_id = $2`,
		servidorID, usuarioID, valor); err != nil {
		return err
	}
	return s.publicarMembro(w, r, servidorID, usuarioID)
}

// exigirAcima garante que quem modera está acima de quem é moderado.
func (s *Servico) exigirAcima(r *http.Request, c *acesso.Contexto, servidorID, usuarioID int64) error {
	if usuarioID == c.Membro.UsuarioID {
		return api.NovoErro(http.StatusBadRequest, "Você não pode fazer isso com você mesmo.")
	}
	alvo, err := acesso.CarregarMembro(r.Context(), s.db, servidorID, usuarioID)
	if err != nil {
		return api.NovoErro(http.StatusNotFound, "Membro não encontrado.")
	}
	if !c.AcimaDe(alvo) {
		return acesso.ErrHierarquia
	}
	return nil
}

type pedidoModeracao struct {
	Motivo  string `json:"motivo"`
	Minutos int    `json:"minutos"` // castigo
}

func lerMotivo(r *http.Request) (pedidoModeracao, error) {
	var p pedidoModeracao
	if r.ContentLength == 0 {
		return p, nil
	}
	if err := api.Ler(r, &p); err != nil {
		return p, err
	}
	p.Motivo = limitar(strings.TrimSpace(p.Motivo), 300)
	return p, nil
}

// Expulsar tira a pessoa do servidor (ela pode voltar com um convite).
func (s *Servico) Expulsar(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.Expulsar); err != nil {
		return err
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	if err := s.exigirAcima(r, c, servidorID, usuarioID); err != nil {
		return err
	}
	p, err := lerMotivo(r)
	if err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "membro_expulso", "usuário "+itoa64(usuarioID), p.Motivo)
	return s.removerMembro(w, r, servidorID, usuarioID, "expulso")
}

// Banir tira a pessoa e impede que volte.
func (s *Servico) Banir(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.Banir); err != nil {
		return err
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	if usuarioID == c.Membro.UsuarioID || usuarioID == c.Servidor.DonoID {
		return api.NovoErro(http.StatusBadRequest, "Essa pessoa não pode ser banida.")
	}
	// Quem ainda é membro precisa estar abaixo; quem já saiu pode ser banido direto.
	if alvo, err := acesso.CarregarMembro(r.Context(), s.db, servidorID, usuarioID); err == nil && !c.AcimaDe(alvo) {
		return acesso.ErrHierarquia
	}
	p, err := lerMotivo(r)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO banimentos (servidor_id, usuario_id, motivo, por_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (servidor_id, usuario_id) DO UPDATE SET motivo = EXCLUDED.motivo, por_id = EXCLUDED.por_id`,
		servidorID, usuarioID, p.Motivo, c.Membro.UsuarioID); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return api.NovoErro(http.StatusNotFound, "Usuário não encontrado.")
		}
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "membro_banido", "usuário "+itoa64(usuarioID), p.Motivo)
	return s.removerMembro(w, r, servidorID, usuarioID, "banido")
}

func (s *Servico) Desbanir(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.Banir); err != nil {
		return err
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM banimentos WHERE servidor_id = $1 AND usuario_id = $2`, servidorID, usuarioID); err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "membro_desbanido", "usuário "+itoa64(usuarioID), "")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type Banimento struct {
	UsuarioID    int64     `json:"usuarioId"`
	NomeUsuario  string    `json:"nomeUsuario"`
	NomeExibicao string    `json:"nomeExibicao"`
	Motivo       string    `json:"motivo"`
	CriadoEm     time.Time `json:"criadoEm"`
}

func (s *Servico) ListarBanidos(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.Banir); err != nil {
		return err
	}
	linhas, err := s.db.QueryContext(r.Context(), `
		SELECT u.id, u.nome_usuario, u.nome_exibicao, b.motivo, b.criado_em
		FROM banimentos b JOIN usuarios u ON u.id = b.usuario_id
		WHERE b.servidor_id = $1 ORDER BY b.criado_em DESC`, servidorID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lista := []Banimento{}
	for linhas.Next() {
		var b Banimento
		if err := linhas.Scan(&b.UsuarioID, &b.NomeUsuario, &b.NomeExibicao, &b.Motivo, &b.CriadoEm); err != nil {
			return err
		}
		lista = append(lista, b)
	}
	api.Responder(w, http.StatusOK, lista)
	return linhas.Err()
}

// Castigar deixa a pessoa só lendo por alguns minutos (0 tira o castigo). Máximo de 28 dias.
func (s *Servico) Castigar(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.Castigar); err != nil {
		return err
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	if err := s.exigirAcima(r, c, servidorID, usuarioID); err != nil {
		return err
	}
	p, err := lerMotivo(r)
	if err != nil {
		return err
	}
	if p.Minutos < 0 || p.Minutos > 28*24*60 {
		return api.NovoErro(http.StatusBadRequest, "O castigo vai de 0 minutos a 28 dias.")
	}
	var ate any
	if p.Minutos > 0 {
		ate = s.agora().Add(time.Duration(p.Minutos) * time.Minute)
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE membros SET castigado_ate = $3 WHERE servidor_id = $1 AND usuario_id = $2`,
		servidorID, usuarioID, ate); err != nil {
		return err
	}
	acao := "castigo_removido"
	if p.Minutos > 0 {
		acao = "membro_castigado"
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, acao, "usuário "+itoa64(usuarioID), p.Motivo)
	return s.publicarMembro(w, r, servidorID, usuarioID)
}

// ---- auditoria ----

type RegistroAuditoria struct {
	ID        int64     `json:"id"`
	AutorID   *int64    `json:"autorId"`
	AutorNome string    `json:"autorNome"`
	Acao      string    `json:"acao"`
	Alvo      string    `json:"alvo"`
	Detalhes  string    `json:"detalhes"`
	CriadoEm  time.Time `json:"criadoEm"`
}

func (s *Servico) Auditoria(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.VerAuditoria); err != nil {
		return err
	}
	linhas, err := s.db.QueryContext(r.Context(), `
		SELECT a.id, a.autor_id, COALESCE(u.nome_exibicao, ''), a.acao, a.alvo, a.detalhes, a.criado_em
		FROM auditoria a LEFT JOIN usuarios u ON u.id = a.autor_id
		WHERE a.servidor_id = $1 ORDER BY a.id DESC LIMIT 100`, servidorID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lista := []RegistroAuditoria{}
	for linhas.Next() {
		var a RegistroAuditoria
		var autor sql.NullInt64
		if err := linhas.Scan(&a.ID, &autor, &a.AutorNome, &a.Acao, &a.Alvo, &a.Detalhes, &a.CriadoEm); err != nil {
			return err
		}
		if autor.Valid {
			a.AutorID = &autor.Int64
		}
		lista = append(lista, a)
	}
	api.Responder(w, http.StatusOK, lista)
	return linhas.Err()
}
