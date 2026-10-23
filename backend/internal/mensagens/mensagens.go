package mensagens

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const (
	tamanhoMaximo       = 4000
	porPagina           = 50
	maxAnexosPorMsg     = 10
	maxReacoesDistintas = 20
)

type Servico struct {
	db       *sql.DB
	pub      eventos.Publicador
	armazem  arquivos.Armazem
	maxAnexo int64
	agora    func() time.Time
	limite   Limitador
}

func NovoServico(db *sql.DB, pub eventos.Publicador, armazem arquivos.Armazem, maxAnexoMB int) *Servico {
	return &Servico{
		db: db, pub: pub, armazem: armazem, maxAnexo: int64(maxAnexoMB) << 20, agora: time.Now,
		limite: &limitador{janela: 5 * time.Second, maximo: 8, vistos: map[int64][]time.Time{}},
	}
}

// ---- modelos ----

type Autor struct {
	ID           int64  `json:"id"`
	NomeUsuario  string `json:"nomeUsuario"`
	NomeExibicao string `json:"nomeExibicao"`
}

type Resposta struct {
	ID        int64  `json:"id"`
	AutorID   *int64 `json:"autorId"`
	AutorNome string `json:"autorNome"`
	Trecho    string `json:"trecho"`
}

type Reacao struct {
	Emoji    string  `json:"emoji"`
	Usuarios []int64 `json:"usuarios"`
}

type Anexo struct {
	ID       int64  `json:"id"`
	Nome     string `json:"nome"`
	Tipo     string `json:"tipo"`
	Tamanho  int64  `json:"tamanho"`
	Endereco string `json:"endereco"`
}

type Mensagem struct {
	ID            int64      `json:"id"`
	CanalID       int64      `json:"canalId"`
	ServidorID    int64      `json:"servidorId"`
	Autor         *Autor     `json:"autor"` // nulo se a conta foi apagada
	Conteudo      string     `json:"conteudo"`
	RespondeA     *Resposta  `json:"respondeA,omitempty"`
	Mencoes       []int64    `json:"mencoes"`
	MencionaTodos bool       `json:"mencionaTodos"`
	Reacoes       []Reacao   `json:"reacoes"`
	Anexos        []Anexo    `json:"anexos"`
	EditadaEm     *time.Time `json:"editadaEm,omitempty"`
	FixadaEm      *time.Time `json:"fixadaEm,omitempty"`
	Enquete       *Enquete   `json:"enquete,omitempty"`
	Fio           *Fio       `json:"fio,omitempty"` // fio que nasceu desta mensagem
	CriadaEm      time.Time  `json:"criadaEm"`
}

// ---- utilidades ----

func idDaRota(r *http.Request, nome string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(nome), 10, 64)
	if err != nil || v <= 0 {
		return 0, api.NovoErro(http.StatusNotFound, "Não encontrado.")
	}
	return v, nil
}

// canal carrega o servidor do canal e as permissões de quem pediu.
func (s *Servico) canal(r *http.Request) (*acesso.Contexto, int64, error) {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return nil, 0, err
	}
	var servidorID sql.NullInt64
	var tipo string
	err = s.db.QueryRowContext(r.Context(), `SELECT servidor_id, tipo FROM canais WHERE id = $1`, canalID).Scan(&servidorID, &tipo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, acesso.ErrCanalSumiu
	}
	if err != nil {
		return nil, 0, err
	}
	if tipo == "dm" || tipo == "grupo" {
		c, err := s.contextoDireto(r.Context(), canalID, api.Quem(r).UsuarioID, tipo)
		return c, canalID, err
	}
	if tipo != "texto" && tipo != "fio" {
		return nil, 0, api.NovoErro(http.StatusBadRequest, "Canais de voz não têm mensagens.")
	}
	c, err := acesso.Carregar(r.Context(), s.db, servidorID.Int64, api.Quem(r).UsuarioID)
	if err != nil {
		return nil, 0, err
	}
	return c, canalID, nil
}

func (s *Servico) publicar(ctx context.Context, e eventos.Evento) {
	if e.ServidorID == 0 {
		s.publicarDireto(ctx, e)
		return
	}
	if err := s.pub.Publicar(ctx, eventos.TopicoServidor(e.ServidorID), e); err != nil {
		slog.Error("não foi possível publicar o evento", "tipo", e.Tipo, "erro", err)
	}
}

// eventoDoCanal só chega a quem vê o canal.
func eventoDoCanal(tipo string, servidorID, canalID int64, dados any) eventos.Evento {
	e := eventos.Novo(tipo, servidorID, canalID, dados)
	e.Exige = uint64(permissoes.VerCanal)
	return e
}

// ---- leitura ----

const consultaMensagens = `
	SELECT m.id, m.canal_id, COALESCE(c.servidor_id, 0), m.autor_id, u.nome_usuario, u.nome_exibicao,
	       m.conteudo, m.responde_a, m.menciona_todos, m.editada_em, m.fixada_em, m.criada_em,
	       r.autor_id, COALESCE(ru.nome_exibicao, ''), COALESCE(left(r.conteudo, 120), '')
	FROM mensagens m
	JOIN canais c ON c.id = m.canal_id
	LEFT JOIN usuarios u ON u.id = m.autor_id
	LEFT JOIN mensagens r ON r.id = m.responde_a
	LEFT JOIN usuarios ru ON ru.id = r.autor_id`

func (s *Servico) lerVarias(ctx context.Context, consulta string, args ...any) ([]Mensagem, error) {
	linhas, err := s.db.QueryContext(ctx, consulta, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []Mensagem{}
	for linhas.Next() {
		var m Mensagem
		var autorID, respID, respAutor sql.NullInt64
		var nomeUsuario, nomeExibicao sql.NullString
		var editada, fixada sql.NullTime
		var respNome, respTrecho string
		if err := linhas.Scan(&m.ID, &m.CanalID, &m.ServidorID, &autorID, &nomeUsuario, &nomeExibicao,
			&m.Conteudo, &respID, &m.MencionaTodos, &editada, &fixada, &m.CriadaEm, &respAutor, &respNome, &respTrecho); err != nil {
			return nil, err
		}
		if autorID.Valid {
			m.Autor = &Autor{ID: autorID.Int64, NomeUsuario: nomeUsuario.String, NomeExibicao: nomeExibicao.String}
		}
		if respID.Valid {
			m.RespondeA = &Resposta{ID: respID.Int64, AutorNome: respNome, Trecho: respTrecho}
			if respAutor.Valid {
				m.RespondeA.AutorID = &respAutor.Int64
			}
		}
		if editada.Valid {
			m.EditadaEm = &editada.Time
		}
		if fixada.Valid {
			m.FixadaEm = &fixada.Time
		}
		m.Mencoes, m.Reacoes, m.Anexos = []int64{}, []Reacao{}, []Anexo{}
		lista = append(lista, m)
	}
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	return lista, s.completar(ctx, lista)
}

// completar busca menções, reações e anexos de várias mensagens de uma vez.
func (s *Servico) completar(ctx context.Context, lista []Mensagem) error {
	if len(lista) == 0 {
		return nil
	}
	ids := make([]int64, len(lista))
	pos := make(map[int64]int, len(lista))
	for i, m := range lista {
		ids[i] = m.ID
		pos[m.ID] = i
	}
	linhas, err := s.db.QueryContext(ctx, `SELECT mensagem_id, usuario_id FROM mencoes WHERE mensagem_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return err
	}
	for linhas.Next() {
		var msg, usuario int64
		if err := linhas.Scan(&msg, &usuario); err != nil {
			linhas.Close()
			return err
		}
		lista[pos[msg]].Mencoes = append(lista[pos[msg]].Mencoes, usuario)
	}
	linhas.Close()

	linhas, err = s.db.QueryContext(ctx, `
		SELECT mensagem_id, emoji, array_agg(usuario_id ORDER BY criada_em)
		FROM reacoes WHERE mensagem_id = ANY($1)
		GROUP BY mensagem_id, emoji ORDER BY mensagem_id, min(criada_em)`, pq.Array(ids))
	if err != nil {
		return err
	}
	for linhas.Next() {
		var msg int64
		var r Reacao
		var usuarios pq.Int64Array
		if err := linhas.Scan(&msg, &r.Emoji, &usuarios); err != nil {
			linhas.Close()
			return err
		}
		r.Usuarios = usuarios
		lista[pos[msg]].Reacoes = append(lista[pos[msg]].Reacoes, r)
	}
	linhas.Close()

	if err := s.completarEnquetes(ctx, lista, ids, pos); err != nil {
		return err
	}
	if err := s.completarFios(ctx, lista, ids, pos); err != nil {
		return err
	}

	linhas, err = s.db.QueryContext(ctx, `
		SELECT mensagem_id, id, nome, tipo, tamanho FROM anexos WHERE mensagem_id = ANY($1) ORDER BY id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer linhas.Close()
	for linhas.Next() {
		var msg int64
		var a Anexo
		if err := linhas.Scan(&msg, &a.ID, &a.Nome, &a.Tipo, &a.Tamanho); err != nil {
			return err
		}
		a.Endereco = enderecoAnexo(a.ID, a.Nome)
		lista[pos[msg]].Anexos = append(lista[pos[msg]].Anexos, a)
	}
	return linhas.Err()
}

func (s *Servico) uma(ctx context.Context, id int64) (Mensagem, error) {
	lista, err := s.lerVarias(ctx, consultaMensagens+` WHERE m.id = $1`, id)
	if err != nil {
		return Mensagem{}, err
	}
	if len(lista) == 0 {
		return Mensagem{}, api.NovoErro(http.StatusNotFound, "Mensagem não encontrada.")
	}
	return lista[0], nil
}

// Listar: GET /api/canais/{canal}/mensagens?antes=ID ou ?depois=ID (50 por vez, da mais antiga para a mais nova).
func (s *Servico) Listar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.LerHistorico); err != nil {
		return err
	}
	q := r.URL.Query()
	var lista []Mensagem
	if depois, _ := strconv.ParseInt(q.Get("depois"), 10, 64); depois > 0 {
		// o que a pessoa perdeu (ex.: ao reconectar): em ordem crescente
		lista, err = s.lerVarias(r.Context(), consultaMensagens+` WHERE m.canal_id = $1 AND m.id > $2 ORDER BY m.id LIMIT $3`,
			canalID, depois, porPagina)
	} else {
		antes, _ := strconv.ParseInt(q.Get("antes"), 10, 64)
		if antes <= 0 {
			antes = 1<<63 - 1
		}
		lista, err = s.lerVarias(r.Context(), `SELECT * FROM (`+consultaMensagens+
			` WHERE m.canal_id = $1 AND m.id < $2 ORDER BY m.id DESC LIMIT $3) pagina ORDER BY id`,
			canalID, antes, porPagina)
	}
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// ---- enviar ----

type pedidoMensagem struct {
	Conteudo  string         `json:"conteudo"`
	RespondeA *int64         `json:"respondeA"`
	Anexos    []int64        `json:"anexos"`
	Enquete   *pedidoEnquete `json:"enquete"`
	Nonce     string         `json:"nonce"` // o navegador usa para trocar a mensagem "enviando" pela real
}

func (s *Servico) Enviar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.EnviarMensagens); err != nil {
		return err
	}
	eu := c.Membro.UsuarioID
	var p pedidoMensagem
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	p.Conteudo = strings.TrimSpace(p.Conteudo)
	if utf8.RuneCountInString(p.Conteudo) > tamanhoMaximo {
		return api.NovoErro(http.StatusBadRequest, "A mensagem pode ter até 4000 caracteres.")
	}
	if p.Conteudo == "" && len(p.Anexos) == 0 && p.Enquete == nil {
		return api.NovoErro(http.StatusBadRequest, "A mensagem está vazia.")
	}
	if p.Enquete != nil {
		if err := p.Enquete.validar(); err != nil {
			return err
		}
	}
	if len(p.Anexos) > maxAnexosPorMsg {
		return api.NovoErro(http.StatusBadRequest, "Até 10 anexos por mensagem.")
	}
	perm := c.NoCanal(canalID)
	if len(p.Anexos) > 0 && !perm.Tem(permissoes.AnexarArquivos) {
		return api.NovoErro(http.StatusForbidden, "Você não pode anexar arquivos neste canal.")
	}
	if !s.limite.Permitir(r.Context(), eu) {
		return api.NovoErro(http.StatusTooManyRequests, "Calma! Você está mandando mensagens rápido demais.")
	}
	if err := s.conferirModoLento(r.Context(), canalID, eu, perm); err != nil {
		return err
	}
	mencoes := LerMencoes(p.Conteudo)
	todos := mencoes.Todos && perm.Tem(permissoes.MencionarTodos)

	var id int64
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		if p.RespondeA != nil {
			var mesmoCanal bool
			if err := tx.QueryRowContext(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM mensagens WHERE id = $1 AND canal_id = $2)`, *p.RespondeA, canalID).Scan(&mesmoCanal); err != nil {
				return err
			}
			if !mesmoCanal {
				return api.NovoErro(http.StatusBadRequest, "A mensagem respondida não está neste canal.")
			}
		}
		if err := tx.QueryRowContext(r.Context(), `
			INSERT INTO mensagens (canal_id, autor_id, conteudo, responde_a, menciona_todos)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			canalID, eu, p.Conteudo, p.RespondeA, todos).Scan(&id); err != nil {
			return err
		}
		if len(p.Anexos) > 0 {
			res, err := tx.ExecContext(r.Context(), `
				UPDATE anexos SET mensagem_id = $1
				WHERE id = ANY($2) AND enviado_por = $3 AND servidor_id IS NOT DISTINCT FROM $4 AND mensagem_id IS NULL`,
				id, pq.Array(p.Anexos), eu, nulo(c.ServidorID))
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); int(n) != len(p.Anexos) {
				return api.NovoErro(http.StatusBadRequest, "Algum anexo não foi encontrado (envie de novo).")
			}
		}
		if p.Enquete != nil {
			if err := s.gravarEnquete(r.Context(), tx, id, p.Enquete); err != nil {
				return err
			}
		}
		return s.gravarMencoes(r.Context(), tx, id, c.ServidorID, canalID, mencoes, perm)
	})
	if err != nil {
		return err
	}
	if c.EhDireto() {
		s.reabrirConversa(r.Context(), canalID)
	}
	// A pessoa leu o canal até a própria mensagem.
	s.marcarLido(r.Context(), eu, canalID, id)

	m, err := s.uma(r.Context(), id)
	if err != nil {
		return err
	}
	saida := struct {
		Mensagem
		Nonce string `json:"nonce,omitempty"`
	}{m, limitarTexto(p.Nonce, 40)}
	s.publicar(r.Context(), eventoDoCanal(eventos.MensagemCriada, c.ServidorID, canalID, saida))
	if c.Canais[canalID] == "fio" {
		if f, err := s.umFio(r.Context(), canalID); err == nil {
			s.avisarFio(r.Context(), c.ServidorID, f)
		}
	}
	api.Responder(w, http.StatusCreated, saida)
	return nil
}

// gravarMencoes guarda quem foi mencionado (pessoas do servidor e membros de cargos mencionáveis).
func (s *Servico) gravarMencoes(ctx context.Context, tx *sql.Tx, mensagemID, servidorID, canalID int64, m Mencoes, perm permissoes.Permissao) error {
	if servidorID == 0 { // conversa direta: só quem participa pode ser mencionado
		if len(m.Usuarios) == 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mencoes (mensagem_id, usuario_id)
			SELECT $1, usuario_id FROM conversa_membros WHERE canal_id = $2 AND usuario_id = ANY($3)
			ON CONFLICT DO NOTHING`, mensagemID, canalID, pq.Array(m.Usuarios))
		return err
	}
	if len(m.Usuarios) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mencoes (mensagem_id, usuario_id)
			SELECT $1, usuario_id FROM membros WHERE servidor_id = $2 AND usuario_id = ANY($3)
			ON CONFLICT DO NOTHING`, mensagemID, servidorID, pq.Array(m.Usuarios)); err != nil {
			return err
		}
	}
	if len(m.Cargos) > 0 {
		// Cargo mencionável: qualquer um menciona. Os outros: só quem pode mencionar @todos.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mencoes (mensagem_id, usuario_id)
			SELECT DISTINCT $1, mc.usuario_id
			FROM membros_cargos mc JOIN cargos c ON c.id = mc.cargo_id
			WHERE mc.servidor_id = $2 AND mc.cargo_id = ANY($3) AND (c.mencionavel OR $4)
			LIMIT 1000
			ON CONFLICT DO NOTHING`, mensagemID, servidorID, pq.Array(m.Cargos), perm.Tem(permissoes.MencionarTodos))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Servico) conferirModoLento(ctx context.Context, canalID, usuarioID int64, perm permissoes.Permissao) error {
	if perm.Tem(permissoes.GerenciarMensagens) || perm.Tem(permissoes.GerenciarCanais) {
		return nil
	}
	var lento int
	var ultima sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT c.modo_lento, (SELECT max(criada_em) FROM mensagens WHERE canal_id = c.id AND autor_id = $2)
		FROM canais c WHERE c.id = $1`, canalID, usuarioID).Scan(&lento, &ultima)
	if err != nil {
		return err
	}
	if lento > 0 && ultima.Valid {
		if falta := time.Duration(lento)*time.Second - s.agora().Sub(ultima.Time); falta > 0 {
			return api.NovoErro(http.StatusTooManyRequests,
				"Modo lento: espere mais "+strconv.Itoa(int(falta.Seconds())+1)+" s para mandar outra mensagem.")
		}
	}
	return nil
}

// ---- editar e apagar ----

func (s *Servico) mensagemDoCanal(r *http.Request, canalID int64) (Mensagem, error) {
	id, err := idDaRota(r, "mensagem")
	if err != nil {
		return Mensagem{}, err
	}
	m, err := s.uma(r.Context(), id)
	if err != nil {
		return m, err
	}
	if m.CanalID != canalID {
		return m, api.NovoErro(http.StatusNotFound, "Mensagem não encontrada.")
	}
	return m, nil
}

// Editar: só quem escreveu.
func (s *Servico) Editar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.EnviarMensagens); err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	if m.Autor == nil || m.Autor.ID != c.Membro.UsuarioID {
		return api.NovoErro(http.StatusForbidden, "Só quem escreveu pode editar a mensagem.")
	}
	var p struct {
		Conteudo string `json:"conteudo"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	p.Conteudo = strings.TrimSpace(p.Conteudo)
	if utf8.RuneCountInString(p.Conteudo) > tamanhoMaximo {
		return api.NovoErro(http.StatusBadRequest, "A mensagem pode ter até 4000 caracteres.")
	}
	if p.Conteudo == "" && len(m.Anexos) == 0 && m.Enquete == nil {
		return api.NovoErro(http.StatusBadRequest, "A mensagem não pode ficar vazia. Apague, se quiser.")
	}
	perm := c.NoCanal(canalID)
	mencoes := LerMencoes(p.Conteudo)
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE mensagens SET conteudo = $2, editada_em = now(), menciona_todos = $3 WHERE id = $1`,
			m.ID, p.Conteudo, mencoes.Todos && perm.Tem(permissoes.MencionarTodos)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM mencoes WHERE mensagem_id = $1`, m.ID); err != nil {
			return err
		}
		return s.gravarMencoes(r.Context(), tx, m.ID, c.ServidorID, canalID, mencoes, perm)
	})
	if err != nil {
		return err
	}
	m, err = s.uma(r.Context(), m.ID)
	if err != nil {
		return err
	}
	s.publicar(r.Context(), eventoDoCanal(eventos.MensagemEditada, c.ServidorID, canalID, m))
	api.Responder(w, http.StatusOK, m)
	return nil
}

// Apagar: quem escreveu ou quem gerencia mensagens.
func (s *Servico) Apagar(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	m, err := s.mensagemDoCanal(r, canalID)
	if err != nil {
		return err
	}
	minha := m.Autor != nil && m.Autor.ID == c.Membro.UsuarioID
	if !minha {
		if err := c.ExigirNoCanal(canalID, permissoes.GerenciarMensagens); err != nil {
			return err
		}
	} else if !c.NoCanal(canalID).Tem(permissoes.VerCanal) {
		return acesso.ErrCanalSumiu
	}
	var chaves []string
	linhas, err := s.db.QueryContext(r.Context(), `SELECT chave FROM anexos WHERE mensagem_id = $1`, m.ID)
	if err == nil {
		for linhas.Next() {
			var k string
			if linhas.Scan(&k) == nil {
				chaves = append(chaves, k)
			}
		}
		linhas.Close()
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM mensagens WHERE id = $1`, m.ID); err != nil {
		return err
	}
	for _, k := range chaves {
		if err := s.armazem.Apagar(r.Context(), k); err != nil {
			slog.Warn("anexo ficou no armazenamento", "chave", k, "erro", err)
		}
	}
	if !minha {
		s.auditar(r.Context(), c.ServidorID, c.Membro.UsuarioID, "mensagem_apagada", "canal "+strconv.FormatInt(canalID, 10),
			"de "+nomeAutor(m)+": "+limitarTexto(m.Conteudo, 200))
	}
	s.publicar(r.Context(), eventoDoCanal(eventos.MensagemApagada, c.ServidorID, canalID, map[string]int64{"id": m.ID, "canalId": canalID}))
	if c.Canais[canalID] == "fio" {
		if f, err := s.umFio(r.Context(), canalID); err == nil {
			s.avisarFio(r.Context(), c.ServidorID, f)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func nomeAutor(m Mensagem) string {
	if m.Autor == nil {
		return "conta apagada"
	}
	return m.Autor.NomeExibicao
}

// ---- reações ----

// Reagir: PUT/DELETE /api/canais/{canal}/mensagens/{mensagem}/reacoes/{emoji}
func (s *Servico) Reagir(w http.ResponseWriter, r *http.Request) error { return s.reacao(w, r, true) }
func (s *Servico) Desreagir(w http.ResponseWriter, r *http.Request) error {
	return s.reacao(w, r, false)
}

func (s *Servico) reacao(w http.ResponseWriter, r *http.Request, adicionar bool) error {
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
	emoji := strings.TrimSpace(r.PathValue("emoji"))
	if emoji == "" || utf8.RuneCountInString(emoji) > 8 || len(emoji) > 32 {
		if !emojiProprio.MatchString(emoji) {
			return api.NovoErro(http.StatusBadRequest, "Emoji inválido.")
		}
	}
	if emojiProprio.MatchString(emoji) && adicionar {
		var existe bool
		_ = s.db.QueryRowContext(r.Context(), `SELECT EXISTS (SELECT 1 FROM emojis WHERE id = $1)`, emoji[2:]).Scan(&existe)
		if !existe {
			return api.NovoErro(http.StatusBadRequest, "Esse emoji não existe mais.")
		}
	}
	eu := c.Membro.UsuarioID
	tipo := eventos.ReacaoRemovida
	if adicionar {
		// Reagir com um emoji novo exige a permissão; somar a uma reação que já existe, não.
		existe := false
		for _, re := range m.Reacoes {
			if re.Emoji == emoji {
				existe = true
			}
		}
		if !existe {
			if !c.NoCanal(canalID).Tem(permissoes.AdicionarReacoes) {
				return api.NovoErro(http.StatusForbidden, "Você não pode adicionar reações neste canal.")
			}
			if len(m.Reacoes) >= maxReacoesDistintas {
				return api.NovoErro(http.StatusBadRequest, "Esta mensagem já tem 20 reações diferentes.")
			}
		}
		if _, err := s.db.ExecContext(r.Context(),
			`INSERT INTO reacoes (mensagem_id, usuario_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, m.ID, eu, emoji); err != nil {
			return err
		}
		tipo = eventos.ReacaoAdicionada
	} else if _, err := s.db.ExecContext(r.Context(),
		`DELETE FROM reacoes WHERE mensagem_id = $1 AND usuario_id = $2 AND emoji = $3`, m.ID, eu, emoji); err != nil {
		return err
	}
	s.publicar(r.Context(), eventoDoCanal(tipo, c.ServidorID, canalID,
		map[string]any{"mensagemId": m.ID, "canalId": canalID, "usuarioId": eu, "emoji": emoji}))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- lido ----

// MarcarLido: POST /api/canais/{canal}/lido {"mensagemId": 123}
func (s *Servico) MarcarLido(w http.ResponseWriter, r *http.Request) error {
	c, canalID, err := s.canal(r)
	if err != nil {
		return err
	}
	if !c.NoCanal(canalID).Tem(permissoes.VerCanal) {
		return acesso.ErrCanalSumiu
	}
	var p struct {
		MensagemID int64 `json:"mensagemId"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	s.marcarLido(r.Context(), c.Membro.UsuarioID, canalID, p.MensagemID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Servico) marcarLido(ctx context.Context, usuarioID, canalID, mensagemID int64) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO leituras (usuario_id, canal_id, ultima_lida) VALUES ($1, $2, $3)
		ON CONFLICT (usuario_id, canal_id) DO UPDATE SET ultima_lida = GREATEST(leituras.ultima_lida, EXCLUDED.ultima_lida)`,
		usuarioID, canalID, mensagemID); err != nil {
		slog.Warn("não foi possível marcar como lido", "erro", err)
	}
}

// ---- busca ----

// Buscar: GET /api/servidores/{servidor}/busca?q=texto&autor=ID&canal=ID
// Procura só nos canais que a pessoa pode ler, com a busca de texto do PostgreSQL (em português).
func (s *Servico) Buscar(w http.ResponseWriter, r *http.Request) error {
	servidorID, err := idDaRota(r, "servidor")
	if err != nil {
		return err
	}
	c, err := acesso.Carregar(r.Context(), s.db, servidorID, api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	termo := strings.TrimSpace(q.Get("q"))
	if utf8.RuneCountInString(termo) < 2 || utf8.RuneCountInString(termo) > 200 {
		return api.NovoErro(http.StatusBadRequest, "Busque por 2 a 200 caracteres.")
	}
	var canais []int64
	filtroCanal, _ := strconv.ParseInt(q.Get("canal"), 10, 64)
	for canalID, tipo := range c.Canais {
		if tipo == "texto" && c.NoCanal(canalID).Tem(permissoes.LerHistorico) && (filtroCanal == 0 || filtroCanal == canalID) {
			canais = append(canais, canalID)
		}
	}
	if len(canais) == 0 {
		api.Responder(w, http.StatusOK, []Mensagem{})
		return nil
	}
	autor, _ := strconv.ParseInt(q.Get("autor"), 10, 64)
	lista, err := s.lerVarias(r.Context(), consultaMensagens+`
		WHERE m.canal_id = ANY($1)
		  AND m.busca @@ websearch_to_tsquery('portuguese', $2)
		  AND ($3 = 0 OR m.autor_id = $3)
		ORDER BY ts_rank(m.busca, websearch_to_tsquery('portuguese', $2)) DESC, m.id DESC
		LIMIT 25`, pq.Array(canais), termo, autor)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// Mencoes: GET /api/eu/mencoes — as últimas mensagens que mencionaram a pessoa (nos canais que ela ainda vê).
func (s *Servico) MinhasMencoes(w http.ResponseWriter, r *http.Request) error {
	eu := api.Quem(r).UsuarioID
	lista, err := s.lerVarias(r.Context(), consultaMensagens+`
		JOIN mencoes mc ON mc.mensagem_id = m.id AND mc.usuario_id = $1
		ORDER BY m.id DESC LIMIT 50`, eu)
	if err != nil {
		return err
	}
	// filtra pelo que a pessoa ainda pode ver (pode ter perdido acesso ao canal)
	contextos := map[int64]*acesso.Contexto{}
	visiveis := []Mensagem{}
	for _, m := range lista {
		c, ok := contextos[m.ServidorID]
		if !ok {
			c, _ = acesso.Carregar(r.Context(), s.db, m.ServidorID, eu)
			contextos[m.ServidorID] = c
		}
		if c != nil && c.NoCanal(m.CanalID).Tem(permissoes.LerHistorico) {
			visiveis = append(visiveis, m)
		}
	}
	api.Responder(w, http.StatusOK, visiveis)
	return nil
}

// ---- utilidades ----

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

func (s *Servico) auditar(ctx context.Context, servidorID, autorID int64, acao, alvo, detalhes string) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO auditoria (servidor_id, autor_id, acao, alvo, detalhes) VALUES ($1, $2, $3, $4, $5)`,
		servidorID, autorID, acao, limitarTexto(alvo, 120), limitarTexto(detalhes, 500)); err != nil {
		slog.Error("não foi possível registrar a auditoria", "erro", err)
	}
}

func limitarTexto(texto string, max int) string {
	r := []rune(texto)
	if len(r) > max {
		return string(r[:max])
	}
	return texto
}

// Limitador decide se a pessoa ainda pode mandar mensagem agora (contra flood).
type Limitador interface {
	Permitir(ctx context.Context, usuarioID int64) bool
}

// UsarLimitador troca o limitador em memória (ex.: pelo do Redis, que vale para todas as instâncias).
func (s *Servico) UsarLimitador(l Limitador) { s.limite = l }

func (l *limitador) Permitir(_ context.Context, usuarioID int64) bool {
	return l.permitir(usuarioID, time.Now())
}

// limitador: até "maximo" mensagens por "janela" por pessoa, nesta instância.
type limitador struct {
	mu     sync.Mutex
	janela time.Duration
	maximo int
	vistos map[int64][]time.Time
}

func (l *limitador) permitir(usuarioID int64, agora time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.vistos) > 50_000 {
		l.vistos = map[int64][]time.Time{}
	}
	recentes := l.vistos[usuarioID][:0]
	for _, t := range l.vistos[usuarioID] {
		if agora.Sub(t) < l.janela {
			recentes = append(recentes, t)
		}
	}
	if len(recentes) >= l.maximo {
		l.vistos[usuarioID] = recentes
		return false
	}
	l.vistos[usuarioID] = append(recentes, agora)
	return true
}
