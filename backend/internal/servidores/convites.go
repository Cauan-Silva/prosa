package servidores

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

type Convite struct {
	Codigo       string     `json:"codigo"`
	ServidorID   int64      `json:"servidorId"`
	ServidorNome string     `json:"servidorNome"`
	CriadorID    int64      `json:"criadorId"`
	CriadorNome  string     `json:"criadorNome"`
	Usos         int        `json:"usos"`
	MaxUsos      *int       `json:"maxUsos"`
	ExpiraEm     *time.Time `json:"expiraEm"`
	CriadoEm     time.Time  `json:"criadoEm"`
	TotalMembros int        `json:"totalMembros,omitempty"`
}

const letrasConvite = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func novoCodigoConvite() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letrasConvite[int(b[i])%len(letrasConvite)]
	}
	return string(b)
}

const colunasConvite = `c.codigo, c.servidor_id, s.nome, c.criador_id, u.nome_exibicao, c.usos, c.max_usos, c.expira_em, c.criado_em`

func lerConvite(l interface{ Scan(...any) error }) (Convite, error) {
	var c Convite
	var max sql.NullInt32
	var expira sql.NullTime
	err := l.Scan(&c.Codigo, &c.ServidorID, &c.ServidorNome, &c.CriadorID, &c.CriadorNome, &c.Usos, &max, &expira, &c.CriadoEm)
	if max.Valid {
		v := int(max.Int32)
		c.MaxUsos = &v
	}
	if expira.Valid {
		c.ExpiraEm = &expira.Time
	}
	return c, err
}

// CriarConvite: horas = 0 nunca expira; maxUsos = 0 sem limite.
func (s *Servico) CriarConvite(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.CriarConvite); err != nil {
		return err
	}
	var p struct {
		Horas   int `json:"horas"`
		MaxUsos int `json:"maxUsos"`
	}
	if r.ContentLength != 0 {
		if err := api.Ler(r, &p); err != nil {
			return err
		}
	}
	if p.Horas < 0 || p.Horas > 24*30 || p.MaxUsos < 0 || p.MaxUsos > 1000 {
		return api.NovoErro(http.StatusBadRequest, "O convite vale até 30 dias e até 1000 usos.")
	}
	var expira, max any
	if p.Horas > 0 {
		expira = s.agora().Add(time.Duration(p.Horas) * time.Hour)
	}
	if p.MaxUsos > 0 {
		max = p.MaxUsos
	}
	for tentativa := 0; tentativa < 5; tentativa++ {
		codigo := novoCodigoConvite()
		_, err = s.db.ExecContext(r.Context(),
			`INSERT INTO convites (codigo, servidor_id, criador_id, max_usos, expira_em) VALUES ($1, $2, $3, $4, $5)`,
			codigo, servidorID, c.Membro.UsuarioID, max, expira)
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			continue
		}
		if err != nil {
			return err
		}
		conv, err := s.convite(r, codigo)
		if err != nil {
			return err
		}
		api.Responder(w, http.StatusCreated, conv)
		return nil
	}
	return errors.New("não foi possível gerar um código de convite livre")
}

func (s *Servico) convite(r *http.Request, codigo string) (Convite, error) {
	c, err := lerConvite(s.db.QueryRowContext(r.Context(), `
		SELECT `+colunasConvite+`
		FROM convites c JOIN servidores s ON s.id = c.servidor_id JOIN usuarios u ON u.id = c.criador_id
		WHERE c.codigo = $1`, strings.TrimSpace(codigo)))
	if errors.Is(err, sql.ErrNoRows) {
		return c, api.NovoErro(http.StatusNotFound, "Convite inválido ou expirado.")
	}
	return c, err
}

func (s *Servico) valido(c Convite) bool {
	if c.ExpiraEm != nil && !s.agora().Before(*c.ExpiraEm) {
		return false
	}
	return c.MaxUsos == nil || c.Usos < *c.MaxUsos
}

// VerConvite mostra para onde o convite leva (antes de aceitar).
func (s *Servico) VerConvite(w http.ResponseWriter, r *http.Request) error {
	c, err := s.convite(r, r.PathValue("codigo"))
	if err != nil {
		return err
	}
	if !s.valido(c) {
		return api.NovoErro(http.StatusNotFound, "Convite inválido ou expirado.")
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM membros WHERE servidor_id = $1`, c.ServidorID).Scan(&c.TotalMembros); err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, c)
	return nil
}

// AceitarConvite faz a pessoa entrar no servidor (o contador de usos é atualizado na mesma transação).
func (s *Servico) AceitarConvite(w http.ResponseWriter, r *http.Request) error {
	eu := api.Quem(r).UsuarioID
	codigo := strings.TrimSpace(r.PathValue("codigo"))
	var servidorID int64
	jaEra := false
	err := s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		var max sql.NullInt32
		var expira sql.NullTime
		var usos int
		err := tx.QueryRowContext(r.Context(),
			`SELECT servidor_id, usos, max_usos, expira_em FROM convites WHERE codigo = $1 FOR UPDATE`, codigo).
			Scan(&servidorID, &usos, &max, &expira)
		invalido := api.NovoErro(http.StatusNotFound, "Convite inválido ou expirado.")
		if errors.Is(err, sql.ErrNoRows) {
			return invalido
		}
		if err != nil {
			return err
		}
		if (expira.Valid && !s.agora().Before(expira.Time)) || (max.Valid && usos >= int(max.Int32)) {
			return invalido
		}
		var banido bool
		if err := tx.QueryRowContext(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM banimentos WHERE servidor_id = $1 AND usuario_id = $2)`, servidorID, eu).Scan(&banido); err != nil {
			return err
		}
		if banido {
			return api.NovoErro(http.StatusForbidden, "Você foi banido deste servidor.")
		}
		res, err := tx.ExecContext(r.Context(),
			`INSERT INTO membros (servidor_id, usuario_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, servidorID, eu)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			jaEra = true
			return nil
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE convites SET usos = usos + 1 WHERE codigo = $1`, codigo)
		return err
	})
	if err != nil {
		return err
	}
	if !jaEra {
		m, err := s.membro(r.Context(), servidorID, eu)
		if err != nil {
			return err
		}
		e := eventos.Novo(eventos.MembroEntrou, servidorID, 0, m)
		e.UsuarioID = eu
		s.publicar(r.Context(), e)
		// a própria pessoa (em outras abas) passa a acompanhar o servidor
		s.publicarUsuario(r.Context(), eu, eventos.Evento{Tipo: eventos.ServidorCriado, ServidorID: servidorID, UsuarioID: eu})
	}
	v, err := s.Visao(r.Context(), servidorID, eu)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, v)
	return nil
}

func (s *Servico) ListarConvites(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	linhas, err := s.db.QueryContext(r.Context(), `
		SELECT `+colunasConvite+`
		FROM convites c JOIN servidores s ON s.id = c.servidor_id JOIN usuarios u ON u.id = c.criador_id
		WHERE c.servidor_id = $1 ORDER BY c.criado_em DESC`, servidorID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lista := []Convite{}
	for linhas.Next() {
		conv, err := lerConvite(linhas)
		if err != nil {
			return err
		}
		if s.valido(conv) {
			lista = append(lista, conv)
		}
	}
	api.Responder(w, http.StatusOK, lista)
	return linhas.Err()
}

func (s *Servico) ApagarConvite(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM convites WHERE codigo = $1 AND servidor_id = $2`,
		r.PathValue("codigo"), servidorID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
