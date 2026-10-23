package mensagens

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Fios (threads) são canais filhos de um canal de texto, criados a partir de uma mensagem.
// As mensagens de um fio usam as rotas de sempre (/api/canais/{fio}/mensagens...).

const FioAtualizado = "FIO_ATUALIZADO" // um fio foi criado, renomeado, apagado ou ganhou mensagem

// Fio é o resumo que aparece embaixo da mensagem de origem e na lista de fios do canal.
type Fio struct {
	ID          int64      `json:"id"`
	Nome        string     `json:"nome"`
	CanalPaiID  int64      `json:"canalPaiId"`
	MensagemID  *int64     `json:"mensagemId,omitempty"` // mensagem de origem (some se for apagada)
	DonoID      *int64     `json:"donoId,omitempty"`
	Total       int        `json:"total"` // mensagens no fio
	UltimaMsgID int64      `json:"ultimaMensagemId"`
	UltimaEm    *time.Time `json:"ultimaEm,omitempty"`
	CriadoEm    time.Time  `json:"criadoEm"`
	Apagado     bool       `json:"apagado,omitempty"` // só no evento de fio apagado
}

const consultaFios = `
	SELECT f.id, f.nome, f.pai_id, f.mensagem_origem, f.dono_id, f.criado_em,
	       count(m.id), COALESCE(max(m.id), 0), max(m.criada_em)
	FROM canais f LEFT JOIN mensagens m ON m.canal_id = f.id
	WHERE f.tipo = 'fio' AND `

func lerFios(linhas *sql.Rows) ([]Fio, error) {
	defer linhas.Close()
	lista := []Fio{}
	for linhas.Next() {
		var f Fio
		var origem, dono sql.NullInt64
		var ultima sql.NullTime
		if err := linhas.Scan(&f.ID, &f.Nome, &f.CanalPaiID, &origem, &dono, &f.CriadoEm, &f.Total, &f.UltimaMsgID, &ultima); err != nil {
			return nil, err
		}
		if origem.Valid {
			f.MensagemID = &origem.Int64
		}
		if dono.Valid {
			f.DonoID = &dono.Int64
		}
		if ultima.Valid {
			f.UltimaEm = &ultima.Time
		}
		lista = append(lista, f)
	}
	return lista, linhas.Err()
}

func (s *Servico) umFio(ctx context.Context, id int64) (Fio, error) {
	linhas, err := s.db.QueryContext(ctx, consultaFios+`f.id = $1 GROUP BY f.id`, id)
	if err != nil {
		return Fio{}, err
	}
	lista, err := lerFios(linhas)
	if err != nil {
		return Fio{}, err
	}
	if len(lista) == 0 {
		return Fio{}, api.NovoErro(http.StatusNotFound, "Fio não encontrado.")
	}
	return lista[0], nil
}

// completarFios põe o resumo do fio nas mensagens que abriram um.
func (s *Servico) completarFios(ctx context.Context, lista []Mensagem, ids []int64, pos map[int64]int) error {
	linhas, err := s.db.QueryContext(ctx, consultaFios+`f.mensagem_origem = ANY($1) GROUP BY f.id`, pq.Array(ids))
	if err != nil {
		return err
	}
	fios, err := lerFios(linhas)
	if err != nil {
		return err
	}
	for i := range fios {
		if f := fios[i]; f.MensagemID != nil {
			lista[pos[*f.MensagemID]].Fio = &fios[i]
		}
	}
	return nil
}

// avisarFio manda o resumo atualizado para quem vê o canal pai.
func (s *Servico) avisarFio(ctx context.Context, servidorID int64, f Fio) {
	s.publicar(ctx, eventoDoCanal(FioAtualizado, servidorID, f.CanalPaiID, f))
}

// CriarFio: POST /api/canais/{canal}/mensagens/{mensagem}/fio {"nome": "..."} — se a mensagem
// já tem fio, devolve o que existe.
func (s *Servico) CriarFio(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if c.EhDireto() || c.Canais[canalID] != "texto" {
		return api.NovoErro(http.StatusBadRequest, "Fios só podem ser criados em canais de texto de servidores.")
	}
	if err := c.ExigirNoCanal(canalID, permissoes.EnviarMensagens); err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := strings.Join(strings.Fields(p.Nome), " ")
	if nome == "" {
		nome = strings.Join(strings.Fields(m.Conteudo), " ")
		if m.Enquete != nil && nome == "" {
			nome = m.Enquete.Pergunta
		}
	}
	if utf8.RuneCountInString(nome) > 40 {
		nome = strings.TrimSpace(string([]rune(nome)[:39])) + "…"
	}
	if nome == "" {
		nome = "Fio"
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO canais (servidor_id, nome, tipo, pai_id, mensagem_origem, dono_id)
		VALUES ($1, $2, 'fio', $3, $4, $5)
		ON CONFLICT (mensagem_origem) WHERE mensagem_origem IS NOT NULL DO NOTHING
		RETURNING id`, c.ServidorID, nome, canalID, m.ID, c.Membro.UsuarioID).Scan(&id)
	status := http.StatusCreated
	if errors.Is(err, sql.ErrNoRows) { // já existia
		status = http.StatusOK
		err = s.db.QueryRowContext(r.Context(), `SELECT id FROM canais WHERE mensagem_origem = $1`, m.ID).Scan(&id)
	}
	if err != nil {
		return err
	}
	f, err := s.umFio(r.Context(), id)
	if err != nil {
		return err
	}
	if status == http.StatusCreated {
		s.avisarFio(r.Context(), c.ServidorID, f)
	}
	api.Responder(w, status, f)
	return nil
}

// ListarFios: GET /api/canais/{canal}/fios — os fios do canal, do mais movimentado ao mais parado.
func (s *Servico) ListarFios(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.LerHistorico); err != nil {
		return err
	}
	linhas, err := s.db.QueryContext(r.Context(), consultaFios+`f.pai_id = $1 GROUP BY f.id ORDER BY COALESCE(max(m.id), 0) DESC, f.id DESC LIMIT 100`, canalID)
	if err != nil {
		return err
	}
	lista, err := lerFios(linhas)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// fioParaMexer confere se a pessoa pode renomear ou apagar o fio (quem criou ou quem gerencia mensagens).
func (s *Servico) fioParaMexer(r *http.Request) (*acesso.Contexto, Fio, error) {
	c, fioID, err := s.canal(r)
	if err != nil {
		return nil, Fio{}, err
	}
	if c.Canais[fioID] != "fio" {
		return nil, Fio{}, api.NovoErro(http.StatusNotFound, "Fio não encontrado.")
	}
	f, err := s.umFio(r.Context(), fioID)
	if err != nil {
		return nil, Fio{}, err
	}
	if f.DonoID == nil || *f.DonoID != c.Membro.UsuarioID {
		if err := c.ExigirNoCanal(fioID, permissoes.GerenciarMensagens); err != nil {
			return nil, Fio{}, err
		}
	} else if !c.NoCanal(fioID).Tem(permissoes.VerCanal) {
		return nil, Fio{}, acesso.ErrCanalSumiu
	}
	return c, f, nil
}

// AlterarFio: PATCH /api/canais/{canal}/fio {"nome": "..."}
func (s *Servico) AlterarFio(w http.ResponseWriter, r *http.Request) error {
	c, f, err := s.fioParaMexer(r)
	if err != nil {
		return err
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := strings.Join(strings.Fields(p.Nome), " ")
	if n := utf8.RuneCountInString(nome); n < 1 || n > 40 {
		return api.NovoErro(http.StatusBadRequest, "O nome do fio precisa ter de 1 a 40 caracteres.")
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE canais SET nome = $2 WHERE id = $1`, f.ID, nome); err != nil {
		return err
	}
	if f, err = s.umFio(r.Context(), f.ID); err != nil {
		return err
	}
	s.avisarFio(r.Context(), c.ServidorID, f)
	api.Responder(w, http.StatusOK, f)
	return nil
}

// ApagarFio: DELETE /api/canais/{canal}/fio — apaga o fio e as mensagens dele.
func (s *Servico) ApagarFio(w http.ResponseWriter, r *http.Request) error {
	c, f, err := s.fioParaMexer(r)
	if err != nil {
		return err
	}
	var chaves pq.StringArray
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(array_agg(a.chave), '{}') FROM anexos a JOIN mensagens m ON m.id = a.mensagem_id WHERE m.canal_id = $1`, f.ID).Scan(&chaves)
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM canais WHERE id = $1`, f.ID); err != nil {
		return err
	}
	for _, k := range chaves {
		if err := s.armazem.Apagar(r.Context(), k); err != nil {
			slog.Warn("anexo do fio ficou no armazenamento", "chave", k, "erro", err)
		}
	}
	if f.DonoID == nil || *f.DonoID != c.Membro.UsuarioID {
		s.auditar(r.Context(), c.ServidorID, c.Membro.UsuarioID, "fio_apagado", "canal "+strconv.FormatInt(f.CanalPaiID, 10), f.Nome)
	}
	f.Apagado = true
	s.avisarFio(r.Context(), c.ServidorID, f)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
