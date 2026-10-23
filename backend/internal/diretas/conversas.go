package diretas

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Conversa é uma DM ou um grupo, do jeito que aparece na lista.
type Conversa struct {
	ID            int64    `json:"id"`
	Tipo          string   `json:"tipo"` // dm | grupo
	Nome          string   `json:"nome"` // só grupos (DM mostra o nome da outra pessoa)
	DonoID        *int64   `json:"donoId,omitempty"`
	Participantes []Pessoa `json:"participantes"`
	UltimaMsgID   int64    `json:"ultimaMensagemId"`
	UltimaLida    int64    `json:"ultimaLida"`
	NaoLidas      int      `json:"naoLidas"` // mensagens dos outros depois da última lida (até 100)
	Permissoes    uint64   `json:"permissoes"`
}

var errNaoAchou = api.NovoErro(http.StatusNotFound, "Conversa não encontrada.")

// conversas lê as conversas abertas de alguém (ou só uma, se canalID > 0).
func (s *Servico) conversas(ctx context.Context, eu, canalID int64) ([]Conversa, error) {
	filtro := `cm.aberta`
	args := []any{eu}
	if canalID > 0 {
		filtro = `c.id = $2`
		args = append(args, canalID)
	}
	linhas, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.tipo, c.nome, c.dono_id,
		       COALESCE((SELECT max(id) FROM mensagens WHERE canal_id = c.id), 0),
		       COALESCE((SELECT ultima_lida FROM leituras WHERE canal_id = c.id AND usuario_id = $1), 0),
		       (SELECT count(*) FROM (SELECT 1 FROM mensagens m WHERE m.canal_id = c.id AND m.autor_id IS DISTINCT FROM $1
		           AND m.id > COALESCE((SELECT ultima_lida FROM leituras WHERE canal_id = c.id AND usuario_id = $1), 0) LIMIT 100) t),
		       c.tipo = 'dm' AND EXISTS (
		           SELECT 1 FROM conversa_membros o JOIN bloqueios b
		             ON (b.quem_id = $1 AND b.bloqueado_id = o.usuario_id) OR (b.quem_id = o.usuario_id AND b.bloqueado_id = $1)
		           WHERE o.canal_id = c.id AND o.usuario_id <> $1)
		FROM conversa_membros cm JOIN canais c ON c.id = cm.canal_id
		WHERE cm.usuario_id = $1 AND `+filtro+`
		ORDER BY 5 DESC, c.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	lista := []Conversa{}
	pos := map[int64]int{}
	for linhas.Next() {
		var c Conversa
		var dono sql.NullInt64
		var bloqueada bool
		if err := linhas.Scan(&c.ID, &c.Tipo, &c.Nome, &dono, &c.UltimaMsgID, &c.UltimaLida, &c.NaoLidas, &bloqueada); err != nil {
			linhas.Close()
			return nil, err
		}
		if dono.Valid {
			c.DonoID = &dono.Int64
		}
		c.Permissoes = uint64(acesso.PermissoesDiretas)
		if bloqueada {
			c.Permissoes = uint64(permissoes.VerCanal | permissoes.LerHistorico)
		}
		c.Participantes = []Pessoa{}
		pos[c.ID] = len(lista)
		lista = append(lista, c)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil || len(lista) == 0 {
		return lista, err
	}
	ids := make([]int64, len(lista))
	for i, c := range lista {
		ids[i] = c.ID
	}
	linhas, err = s.db.QueryContext(ctx, `
		SELECT `+colunasPessoa+`, cm.canal_id FROM conversa_membros cm JOIN usuarios u ON u.id = cm.usuario_id
		WHERE cm.canal_id = ANY($1) ORDER BY cm.entrou_em`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var todas []Pessoa
	var de []int64
	for linhas.Next() {
		var canal int64
		p, err := lerPessoa(linhas, &canal)
		if err != nil {
			return nil, err
		}
		todas = append(todas, p)
		de = append(de, canal)
	}
	s.marcarOnline(ctx, todas)
	for i, p := range todas {
		c := &lista[pos[de[i]]]
		c.Participantes = append(c.Participantes, p)
	}
	return lista, linhas.Err()
}

// Listar: GET /api/conversas
func (s *Servico) ListarConversas(w http.ResponseWriter, r *http.Request) error {
	lista, err := s.conversas(r.Context(), api.Quem(r).UsuarioID, 0)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

func (s *Servico) uma(ctx context.Context, eu, canalID int64) (Conversa, error) {
	lista, err := s.conversas(ctx, eu, canalID)
	if err != nil {
		return Conversa{}, err
	}
	if len(lista) == 0 {
		return Conversa{}, errNaoAchou
	}
	return lista[0], nil
}

// Criar: POST /api/conversas {"usuarios": [id], "nome": ""} — uma pessoa abre (ou reabre) a
// DM; duas ou mais criam um grupo (só com amigos).
func (s *Servico) CriarConversa(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Usuarios []int64 `json:"usuarios"`
		Nome     string  `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	vistos := map[int64]bool{eu: true}
	outros := []int64{}
	for _, id := range p.Usuarios {
		if id > 0 && !vistos[id] {
			vistos[id] = true
			outros = append(outros, id)
		}
	}
	switch {
	case len(outros) == 0:
		return api.NovoErro(http.StatusBadRequest, "Escolha com quem conversar.")
	case len(outros) == 1:
		id, err := s.abrirDM(r.Context(), eu, outros[0])
		if err != nil {
			return err
		}
		c, err := s.uma(r.Context(), eu, id)
		if err != nil {
			return err
		}
		api.Responder(w, http.StatusOK, c)
		return nil
	case len(outros)+1 > maxNoGrupo:
		return api.NovoErro(http.StatusBadRequest, "Um grupo pode ter até 10 pessoas.")
	}
	nome, err := nomeDoGrupo(p.Nome)
	if err != nil {
		return err
	}
	for _, id := range outros {
		if ok, err := s.amigos(r.Context(), eu, id); err != nil {
			return err
		} else if !ok {
			return api.NovoErro(http.StatusForbidden, "Só dá para colocar amigos num grupo.")
		}
	}
	var canalID int64
	err = emTransacao(r.Context(), s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO canais (servidor_id, nome, tipo, dono_id) VALUES (NULL, $1, 'grupo', $2) RETURNING id`, nome, eu).Scan(&canalID); err != nil {
			return err
		}
		_, err := tx.ExecContext(r.Context(), `
			INSERT INTO conversa_membros (canal_id, usuario_id) SELECT $1, unnest($2::bigint[])`, canalID, pq.Array(append([]int64{eu}, outros...)))
		return err
	})
	if err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, canalID, nil, append([]int64{eu}, outros...)...)
	c, err := s.uma(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusCreated, c)
	return nil
}

func nomeDoGrupo(nome string) (string, error) {
	nome = strings.Join(strings.Fields(nome), " ")
	if utf8.RuneCountInString(nome) > 40 {
		return "", api.NovoErro(http.StatusBadRequest, "O nome do grupo pode ter até 40 caracteres.")
	}
	return nome, nil
}

// abrirDM devolve a DM entre as duas pessoas, criando se ainda não existir. Pode conversar
// quem é amigo ou divide algum servidor, desde que ninguém tenha bloqueado ninguém.
func (s *Servico) abrirDM(ctx context.Context, eu, alvo int64) (int64, error) {
	if bloq, err := s.bloqueio(ctx, eu, alvo); err != nil {
		return 0, err
	} else if bloq {
		return 0, api.NovoErro(http.StatusForbidden, "Não dá para mandar mensagem para essa pessoa.")
	}
	menor, maior := eu, alvo
	if menor > maior {
		menor, maior = maior, menor
	}
	var canalID int64
	err := s.db.QueryRowContext(ctx, `SELECT canal_id FROM dm_pares WHERE menor = $1 AND maior = $2`, menor, maior).Scan(&canalID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE conversa_membros SET aberta = TRUE WHERE canal_id = $1 AND usuario_id = $2`, canalID, eu)
		return canalID, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var pode bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM usuarios WHERE id = $2) AND (
		       EXISTS (SELECT 1 FROM amizades WHERE estado = 'aceita' AND ((de_id = $1 AND para_id = $2) OR (de_id = $2 AND para_id = $1)))
		    OR EXISTS (SELECT 1 FROM membros a JOIN membros b ON b.servidor_id = a.servidor_id WHERE a.usuario_id = $1 AND b.usuario_id = $2))`,
		eu, alvo).Scan(&pode); err != nil {
		return 0, err
	}
	if !pode {
		return 0, api.NovoErro(http.StatusForbidden, "Para mandar mensagem, vocês precisam ser amigos ou estar no mesmo servidor.")
	}
	err = emTransacao(ctx, s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO canais (servidor_id, nome, tipo) VALUES (NULL, '', 'dm') RETURNING id`).Scan(&canalID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO dm_pares (menor, maior, canal_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, menor, maior, canalID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errDMJaExiste
		}
		// a outra pessoa só vê a conversa quando chegar a primeira mensagem
		_, err = tx.ExecContext(ctx, `
			INSERT INTO conversa_membros (canal_id, usuario_id, aberta) VALUES ($1, $2, TRUE), ($1, $3, FALSE)`, canalID, eu, alvo)
		return err
	})
	if errors.Is(err, errDMJaExiste) { // as duas pessoas abriram ao mesmo tempo
		return s.abrirDM(ctx, eu, alvo)
	}
	return canalID, err
}

var errDMJaExiste = errors.New("dm já existe")

// participantes de uma conversa de que eu faço parte (e o tipo dela).
func (s *Servico) participantes(ctx context.Context, eu, canalID int64) (string, *int64, []int64, error) {
	var tipo string
	var dono sql.NullInt64
	var ids pq.Int64Array
	err := s.db.QueryRowContext(ctx, `
		SELECT c.tipo, c.dono_id, array_agg(cm.usuario_id ORDER BY cm.entrou_em)
		FROM canais c JOIN conversa_membros cm ON cm.canal_id = c.id
		WHERE c.id = $1 AND c.tipo IN ('dm', 'grupo') GROUP BY c.id`, canalID).Scan(&tipo, &dono, &ids)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil, errNaoAchou
	}
	if err != nil {
		return "", nil, nil, err
	}
	for _, id := range ids {
		if id == eu {
			var d *int64
			if dono.Valid {
				d = &dono.Int64
			}
			return tipo, d, ids, nil
		}
	}
	return "", nil, nil, errNaoAchou
}

// AlterarConversa: PATCH /api/conversas/{canal} {"nome": "..."} (grupos; qualquer participante).
func (s *Servico) AlterarConversa(w http.ResponseWriter, r *http.Request) error {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	tipo, _, ids, err := s.participantes(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	if tipo != "grupo" {
		return api.NovoErro(http.StatusBadRequest, "Só grupos têm nome.")
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome, err := nomeDoGrupo(p.Nome)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE canais SET nome = $2 WHERE id = $1`, canalID, nome); err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, canalID, nil, ids...)
	c, err := s.uma(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, c)
	return nil
}

// AdicionarAoGrupo: POST /api/conversas/{canal}/membros {"usuarioId": 3} (só amigos de quem adiciona).
func (s *Servico) AdicionarAoGrupo(w http.ResponseWriter, r *http.Request) error {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	tipo, _, ids, err := s.participantes(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	if tipo != "grupo" {
		return api.NovoErro(http.StatusBadRequest, "Para conversar em mais gente, crie um grupo.")
	}
	var p struct {
		UsuarioID int64 `json:"usuarioId"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	if len(ids) >= maxNoGrupo {
		return api.NovoErro(http.StatusBadRequest, "O grupo já tem 10 pessoas.")
	}
	if ok, err := s.amigos(r.Context(), eu, p.UsuarioID); err != nil {
		return err
	} else if !ok {
		return api.NovoErro(http.StatusForbidden, "Só dá para adicionar amigos ao grupo.")
	}
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO conversa_membros (canal_id, usuario_id) VALUES ($1, $2) ON CONFLICT (canal_id, usuario_id) DO UPDATE SET aberta = TRUE`,
		canalID, p.UsuarioID); err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, canalID, nil, append(ids, p.UsuarioID)...)
	c, err := s.uma(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, c)
	return nil
}

// TirarDoGrupo: DELETE /api/conversas/{canal}/membros/{usuario} — sair (a própria pessoa) ou
// tirar alguém (só quem criou). Se quem criou sai, o grupo passa para quem está há mais tempo;
// se não sobra ninguém, o grupo é apagado.
func (s *Servico) TirarDoGrupo(w http.ResponseWriter, r *http.Request) error {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return err
	}
	alvo, err := idDaRota(r, "usuario")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	tipo, dono, ids, err := s.participantes(r.Context(), eu, canalID)
	if err != nil {
		return err
	}
	if tipo != "grupo" {
		return api.NovoErro(http.StatusBadRequest, "Numa DM, use \"fechar conversa\".")
	}
	if alvo != eu && (dono == nil || *dono != eu) {
		return api.NovoErro(http.StatusForbidden, "Só quem criou o grupo pode tirar pessoas.")
	}
	err = emTransacao(r.Context(), s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM conversa_membros WHERE canal_id = $1 AND usuario_id = $2`, canalID, alvo); err != nil {
			return err
		}
		var restam int
		if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM conversa_membros WHERE canal_id = $1`, canalID).Scan(&restam); err != nil {
			return err
		}
		if restam == 0 {
			_, err := tx.ExecContext(r.Context(), `DELETE FROM canais WHERE id = $1`, canalID)
			return err
		}
		if dono != nil && *dono == alvo {
			_, err := tx.ExecContext(r.Context(), `
				UPDATE canais SET dono_id = (SELECT usuario_id FROM conversa_membros WHERE canal_id = $1 ORDER BY entrou_em LIMIT 1)
				WHERE id = $1`, canalID)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, canalID, map[string]int64{"saiu": alvo}, ids...)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Fechar: DELETE /api/conversas/{canal} — tira a DM da lista (volta quando chegar mensagem).
func (s *Servico) FecharConversa(w http.ResponseWriter, r *http.Request) error {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	if _, _, _, err := s.participantes(r.Context(), eu, canalID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE conversa_membros SET aberta = FALSE WHERE canal_id = $1 AND usuario_id = $2`, canalID, eu); err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, canalID, nil, eu)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func emTransacao(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
