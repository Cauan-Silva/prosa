// Package diretas cuida do que não pertence a nenhum servidor: amigos, bloqueios e as
// conversas diretas (DM entre duas pessoas e grupos de até 10).
//
// As conversas são canais sem servidor (tipo "dm" ou "grupo"), então as mensagens usam
// as mesmas rotas dos canais (/api/canais/{canal}/mensagens...). Os eventos de amizade e
// de conversa vão para o tópico pessoal de cada pessoa envolvida.
package diretas

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/presenca"
	"github.com/Cauan-Silva/prosa/internal/usuarios"
)

const (
	AmigoAlterado     = "AMIGO_ALTERADO"    // pedido, aceite, remoção ou bloqueio: o navegador recarrega a lista
	ConversaAlterada  = "CONVERSA_ALTERADA" // conversa criada, renomeada, gente entrou ou saiu
	maxPedidosAbertos = 100
	maxNoGrupo        = 10
)

type Servico struct {
	db       *sql.DB
	pub      eventos.Publicador
	presenca presenca.Registro // nil = ninguém aparece online (testes)
}

func NovoServico(db *sql.DB, pub eventos.Publicador, pres presenca.Registro) *Servico {
	return &Servico{db: db, pub: pub, presenca: pres}
}

// Pessoa é o resumo de alguém nas listas de amigos e nas conversas.
type Pessoa struct {
	ID           int64  `json:"id"`
	NomeUsuario  string `json:"nomeUsuario"`
	NomeExibicao string `json:"nomeExibicao"`
	Avatar       string `json:"avatar"`
	Status       string `json:"status"`
	StatusTexto  string `json:"statusTexto"`
	StatusEmoji  string `json:"statusEmoji"`
	Online       bool   `json:"online"`
}

const colunasPessoa = `u.id, u.nome_usuario, u.nome_exibicao, u.avatar, u.status, u.status_texto, u.status_emoji`

func lerPessoa(l interface{ Scan(...any) error }, extras ...any) (Pessoa, error) {
	var p Pessoa
	err := l.Scan(append([]any{&p.ID, &p.NomeUsuario, &p.NomeExibicao, &p.Avatar, &p.Status, &p.StatusTexto, &p.StatusEmoji}, extras...)...)
	p.Avatar = usuarios.URLImagem(p.Avatar)
	return p, err
}

// marcarOnline preenche Online (quem está invisível aparece offline).
func (s *Servico) marcarOnline(ctx context.Context, lista []Pessoa) {
	if s.presenca == nil || len(lista) == 0 {
		return
	}
	ids := make([]int64, len(lista))
	for i, p := range lista {
		ids[i] = p.ID
	}
	on, err := s.presenca.Online(ctx, ids)
	if err != nil {
		slog.Warn("diretas: presença", "erro", err)
		return
	}
	for i := range lista {
		lista[i].Online = on[lista[i].ID] && lista[i].Status != "invisivel"
		if lista[i].Status == "invisivel" {
			lista[i].Status = "online"
		}
	}
}

// avisar manda um evento (sem servidor) para o tópico pessoal de cada pessoa.
func (s *Servico) avisar(ctx context.Context, tipo string, canalID int64, dados any, pessoas ...int64) {
	e := eventos.Novo(tipo, 0, canalID, dados)
	for _, id := range pessoas {
		if err := s.pub.Publicar(ctx, eventos.TopicoUsuario(id), e); err != nil {
			slog.Warn("diretas: evento", "tipo", tipo, "erro", err)
		}
	}
}

func idDaRota(r *http.Request, nome string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(nome), 10, 64)
	if err != nil || v <= 0 {
		return 0, api.NovoErro(http.StatusNotFound, "Não encontrado.")
	}
	return v, nil
}

// bloqueio diz se existe bloqueio entre as duas pessoas (de qualquer lado).
func (s *Servico) bloqueio(ctx context.Context, a, b int64) (bool, error) {
	var tem bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM bloqueios WHERE (quem_id = $1 AND bloqueado_id = $2) OR (quem_id = $2 AND bloqueado_id = $1))`, a, b).Scan(&tem)
	return tem, err
}

func (s *Servico) amigos(ctx context.Context, a, b int64) (bool, error) {
	var tem bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM amizades WHERE estado = 'aceita' AND ((de_id = $1 AND para_id = $2) OR (de_id = $2 AND para_id = $1)))`, a, b).Scan(&tem)
	return tem, err
}

// AmigosDe lista os amigos de alguém (o gateway avisa a eles quando a pessoa entra ou sai).
func AmigosDe(ctx context.Context, db *sql.DB, id int64) ([]int64, error) {
	var ids pq.Int64Array
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(array_agg(CASE WHEN de_id = $1 THEN para_id ELSE de_id END), '{}')
		FROM amizades WHERE estado = 'aceita' AND (de_id = $1 OR para_id = $1)`, id).Scan(&ids)
	return ids, err
}
