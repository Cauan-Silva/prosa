package mensagens

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// emojiProprio é uma reação com emoji de servidor: "c:" + id do emoji.
var emojiProprio = regexp.MustCompile(`^c:\d{1,19}$`)

// Enquete vai junto da mensagem: pergunta, de 2 a 10 opções e quem votou em cada uma.
type Enquete struct {
	Pergunta  string         `json:"pergunta"`
	Opcoes    []OpcaoEnquete `json:"opcoes"`
	Multipla  bool           `json:"multipla"` // pode escolher mais de uma
	EncerraEm *time.Time     `json:"encerraEm,omitempty"`
	Encerrada bool           `json:"encerrada"`
}

type OpcaoEnquete struct {
	Texto string  `json:"texto"`
	Votos []int64 `json:"votos"`
}

type pedidoEnquete struct {
	Pergunta string   `json:"pergunta"`
	Opcoes   []string `json:"opcoes"`
	Multipla bool     `json:"multipla"`
	Horas    int      `json:"horas"` // 0 = sem prazo
}

// validar limpa e confere a enquete pedida.
func (p *pedidoEnquete) validar() error {
	p.Pergunta = strings.Join(strings.Fields(p.Pergunta), " ")
	if n := utf8.RuneCountInString(p.Pergunta); n < 1 || n > 300 {
		return api.NovoErro(http.StatusBadRequest, "A pergunta da enquete precisa ter de 1 a 300 caracteres.")
	}
	vistas := map[string]bool{}
	limpas := []string{}
	for _, o := range p.Opcoes {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			continue
		}
		if utf8.RuneCountInString(o) > 80 {
			return api.NovoErro(http.StatusBadRequest, "Cada opção da enquete pode ter até 80 caracteres.")
		}
		if vistas[strings.ToLower(o)] {
			return api.NovoErro(http.StatusBadRequest, "A enquete tem opções repetidas.")
		}
		vistas[strings.ToLower(o)] = true
		limpas = append(limpas, o)
	}
	if len(limpas) < 2 || len(limpas) > 10 {
		return api.NovoErro(http.StatusBadRequest, "A enquete precisa de 2 a 10 opções.")
	}
	p.Opcoes = limpas
	if p.Horas < 0 || p.Horas > 24*30 {
		return api.NovoErro(http.StatusBadRequest, "A enquete pode durar até 30 dias.")
	}
	return nil
}

func (s *Servico) gravarEnquete(ctx context.Context, tx *sql.Tx, mensagemID int64, p *pedidoEnquete) error {
	var encerra sql.NullTime
	if p.Horas > 0 {
		encerra = sql.NullTime{Time: s.agora().Add(time.Duration(p.Horas) * time.Hour), Valid: true}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO enquetes (mensagem_id, pergunta, opcoes, multipla, encerra_em) VALUES ($1, $2, $3, $4, $5)`,
		mensagemID, p.Pergunta, pq.Array(p.Opcoes), p.Multipla, encerra)
	return err
}

// completarEnquetes busca as enquetes (e os votos) de várias mensagens de uma vez.
func (s *Servico) completarEnquetes(ctx context.Context, lista []Mensagem, ids []int64, pos map[int64]int) error {
	linhas, err := s.db.QueryContext(ctx, `SELECT mensagem_id, pergunta, opcoes, multipla, encerra_em FROM enquetes WHERE mensagem_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return err
	}
	agora := s.agora()
	achou := false
	for linhas.Next() {
		var id int64
		var e Enquete
		var opcoes pq.StringArray
		var encerra sql.NullTime
		if err := linhas.Scan(&id, &e.Pergunta, &opcoes, &e.Multipla, &encerra); err != nil {
			linhas.Close()
			return err
		}
		for _, o := range opcoes {
			e.Opcoes = append(e.Opcoes, OpcaoEnquete{Texto: o, Votos: []int64{}})
		}
		if encerra.Valid {
			e.EncerraEm = &encerra.Time
			e.Encerrada = !agora.Before(encerra.Time)
		}
		lista[pos[id]].Enquete = &e
		achou = true
	}
	linhas.Close()
	if !achou {
		return nil
	}
	linhas, err = s.db.QueryContext(ctx, `SELECT mensagem_id, opcao, usuario_id FROM votos WHERE mensagem_id = ANY($1) ORDER BY usuario_id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer linhas.Close()
	for linhas.Next() {
		var id, usuario int64
		var opcao int
		if err := linhas.Scan(&id, &opcao, &usuario); err != nil {
			return err
		}
		if e := lista[pos[id]].Enquete; e != nil && opcao >= 0 && opcao < len(e.Opcoes) {
			e.Opcoes[opcao].Votos = append(e.Opcoes[opcao].Votos, usuario)
		}
	}
	return linhas.Err()
}

// Votar: PUT /api/canais/{canal}/mensagens/{mensagem}/votos {"opcoes": [0, 2]} (vazio tira o voto).
func (s *Servico) Votar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.LerHistorico); err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	if m.Enquete == nil {
		return api.NovoErro(http.StatusBadRequest, "Esta mensagem não tem enquete.")
	}
	if m.Enquete.Encerrada {
		return api.NovoErro(http.StatusBadRequest, "A enquete já acabou.")
	}
	var p struct {
		Opcoes []int `json:"opcoes"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	escolhas := map[int]bool{}
	for _, o := range p.Opcoes {
		if o < 0 || o >= len(m.Enquete.Opcoes) {
			return api.NovoErro(http.StatusBadRequest, "Opção inválida.")
		}
		escolhas[o] = true
	}
	if len(escolhas) > 1 && !m.Enquete.Multipla {
		return api.NovoErro(http.StatusBadRequest, "Nesta enquete só dá para escolher uma opção.")
	}
	eu := c.Membro.UsuarioID
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM votos WHERE mensagem_id = $1 AND usuario_id = $2`, m.ID, eu); err != nil {
			return err
		}
		for o := range escolhas {
			if _, err := tx.ExecContext(r.Context(), `INSERT INTO votos (mensagem_id, usuario_id, opcao) VALUES ($1, $2, $3)`, m.ID, eu, o); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.devolverAtualizada(w, r, c.ServidorID, canalID, m.ID)
}

// Encerrar: POST /api/canais/{canal}/mensagens/{mensagem}/encerrar (quem criou ou quem gerencia mensagens).
func (s *Servico) Encerrar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	if m.Enquete == nil {
		return api.NovoErro(http.StatusBadRequest, "Esta mensagem não tem enquete.")
	}
	if m.Autor == nil || m.Autor.ID != c.Membro.UsuarioID {
		if err := c.ExigirNoCanal(canalID, permissoes.GerenciarMensagens); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE enquetes SET encerra_em = $2 WHERE mensagem_id = $1 AND (encerra_em IS NULL OR encerra_em > $2)`, m.ID, s.agora()); err != nil {
		return err
	}
	return s.devolverAtualizada(w, r, c.ServidorID, canalID, m.ID)
}

func (s *Servico) devolverAtualizada(w http.ResponseWriter, r *http.Request, servidorID, canalID, id int64) error {
	m, err := s.uma(r.Context(), id)
	if err != nil {
		return err
	}
	s.publicar(r.Context(), eventoDoCanal(eventos.MensagemEditada, servidorID, canalID, m))
	api.Responder(w, http.StatusOK, m)
	return nil
}
