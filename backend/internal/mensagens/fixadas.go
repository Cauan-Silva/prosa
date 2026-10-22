package mensagens

import (
	"net/http"
	"strconv"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const maxFixadasPorCanal = 50

// Fixar: PUT /api/canais/{canal}/mensagens/{mensagem}/fixar (quem gerencia mensagens).
func (s *Servico) Fixar(w http.ResponseWriter, r *http.Request) error { return s.fixar(w, r, true) }

// Desfixar: DELETE /api/canais/{canal}/mensagens/{mensagem}/fixar
func (s *Servico) Desfixar(w http.ResponseWriter, r *http.Request) error { return s.fixar(w, r, false) }

func (s *Servico) fixar(w http.ResponseWriter, r *http.Request, fixar bool) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.GerenciarMensagens); err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	if fixar && m.FixadaEm == nil {
		var total int
		if err := s.db.QueryRowContext(r.Context(),
			`SELECT count(*) FROM mensagens WHERE canal_id = $1 AND fixada_em IS NOT NULL`, canalID).Scan(&total); err != nil {
			return err
		}
		if total >= maxFixadasPorCanal {
			return api.NovoErro(http.StatusBadRequest, "Este canal já tem 50 mensagens fixadas. Desafixe alguma antes.")
		}
		_, err = s.db.ExecContext(r.Context(), `UPDATE mensagens SET fixada_em = now(), fixada_por = $2 WHERE id = $1`, m.ID, c.Membro.UsuarioID)
	} else if !fixar && m.FixadaEm != nil {
		_, err = s.db.ExecContext(r.Context(), `UPDATE mensagens SET fixada_em = NULL, fixada_por = NULL WHERE id = $1`, m.ID)
	}
	if err != nil {
		return err
	}
	if m, err = s.uma(r.Context(), m.ID); err != nil {
		return err
	}
	acao := "mensagem_fixada"
	if !fixar {
		acao = "mensagem_desafixada"
	}
	s.auditar(r.Context(), c.ServidorID, c.Membro.UsuarioID, acao, "canal "+strconv.FormatInt(canalID, 10),
		"de "+nomeAutor(m)+": "+limitarTexto(m.Conteudo, 200))
	s.publicar(r.Context(), eventoDoCanal(eventos.MensagemEditada, c.ServidorID, canalID, m))
	api.Responder(w, http.StatusOK, m)
	return nil
}

// Fixadas: GET /api/canais/{canal}/fixadas (da fixada mais recente para a mais antiga).
func (s *Servico) Fixadas(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.LerHistorico); err != nil {
		return err
	}
	lista, err := s.lerVarias(r.Context(), consultaMensagens+
		` WHERE m.canal_id = $1 AND m.fixada_em IS NOT NULL ORDER BY m.fixada_em DESC LIMIT $2`, canalID, maxFixadasPorCanal)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}
