package servidores

import (
	"context"
	"database/sql"
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

const maxServidoresPorPessoa = 100

func validarNome(nome string, min, max int, oque string) (string, error) {
	nome = strings.Join(strings.Fields(nome), " ")
	if n := utf8.RuneCountInString(nome); n < min || n > max {
		return "", api.NovoErro(http.StatusBadRequest, oque+" precisa ter de "+itoa(min)+" a "+itoa(max)+" caracteres.")
	}
	return nome, nil
}

// Criar monta um servidor novo com o cargo @todos, duas categorias e um canal de cada tipo.
func (s *Servico) Criar(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome, err := validarNome(p.Nome, 2, 60, "O nome do servidor")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID

	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM membros WHERE usuario_id = $1`, eu).Scan(&total); err != nil {
		return err
	}
	if total >= maxServidoresPorPessoa {
		return api.NovoErro(http.StatusBadRequest, "Você já está em 100 servidores, o máximo.")
	}

	var servidorID int64
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO servidores (nome, dono_id) VALUES ($1, $2) RETURNING id`, nome, eu).Scan(&servidorID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO cargos (servidor_id, nome, permissoes, posicao, padrao) VALUES ($1, '@todos', $2, 0, TRUE)`,
			servidorID, int64(permissoes.Padrao)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO membros (servidor_id, usuario_id) VALUES ($1, $2)`, servidorID, eu); err != nil {
			return err
		}
		var texto, voz int64
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO categorias (servidor_id, nome, posicao) VALUES ($1, 'Canais de texto', 0) RETURNING id`, servidorID).Scan(&texto); err != nil {
			return err
		}
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO categorias (servidor_id, nome, posicao) VALUES ($1, 'Canais de voz', 1) RETURNING id`, servidorID).Scan(&voz); err != nil {
			return err
		}
		_, err := tx.ExecContext(r.Context(), `
			INSERT INTO canais (servidor_id, categoria_id, nome, tipo, posicao) VALUES
			($1, $2, 'geral', 'texto', 0), ($1, $3, 'Sala de voz', 'voz', 0)`, servidorID, texto, voz)
		return err
	})
	if err != nil {
		return err
	}
	s.publicarUsuario(r.Context(), eu, eventos.Evento{Tipo: eventos.ServidorCriado, ServidorID: servidorID, UsuarioID: eu})
	visao, err := s.Visao(r.Context(), servidorID, eu)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusCreated, visao)
	return nil
}

// Meus lista os servidores da pessoa (resumo para a barra lateral).
func (s *Servico) Meus(w http.ResponseWriter, r *http.Request) error {
	linhas, err := s.db.QueryContext(r.Context(), `
		SELECT `+colunasServidor+`
		FROM servidores s JOIN membros m ON m.servidor_id = s.id
		WHERE m.usuario_id = $1 ORDER BY m.entrou_em`, api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lista := []Servidor{}
	for linhas.Next() {
		v, err := lerServidor(linhas)
		if err != nil {
			return err
		}
		lista = append(lista, v)
	}
	if err := linhas.Err(); err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// VisaoServidor é tudo o que a tela precisa para montar um servidor para uma pessoa.
type VisaoServidor struct {
	Servidor   Servidor    `json:"servidor"`
	Cargos     []Cargo     `json:"cargos"`
	Categorias []Categoria `json:"categorias"`
	Canais     []Canal     `json:"canais"` // só os que a pessoa vê
	Eu         struct {
		Permissoes   uint64     `json:"permissoes"`
		Cargos       []int64    `json:"cargos"`
		CastigadoAte *time.Time `json:"castigadoAte,omitempty"`
	} `json:"eu"`
	TotalMembros int `json:"totalMembros"`
}

// Visao monta o servidor como a pessoa o vê (canais escondidos ficam de fora).
func (s *Servico) Visao(ctx context.Context, servidorID, usuarioID int64) (VisaoServidor, error) {
	var v VisaoServidor
	c, err := acesso.Carregar(ctx, s.db, servidorID, usuarioID)
	if err != nil {
		return v, err
	}
	if v.Servidor, err = lerServidor(s.db.QueryRowContext(ctx, `SELECT `+colunasServidor+` FROM servidores s WHERE s.id = $1`, servidorID)); err != nil {
		return v, err
	}
	if v.Cargos, err = s.cargos(ctx, servidorID); err != nil {
		return v, err
	}
	if v.Categorias, err = s.categorias(ctx, servidorID); err != nil {
		return v, err
	}
	todos, err := s.canais(ctx, servidorID)
	if err != nil {
		return v, err
	}
	v.Canais = []Canal{}
	visiveis := []int64{}
	for _, canal := range todos {
		p := c.NoCanal(canal.ID)
		if !p.Tem(permissoes.VerCanal) {
			continue
		}
		canal.Permissoes = uint64(p)
		v.Canais = append(v.Canais, canal)
		visiveis = append(visiveis, canal.ID)
	}
	if err := s.preencherLeitura(ctx, usuarioID, visiveis, v.Canais); err != nil {
		return v, err
	}
	v.Eu.Permissoes = uint64(c.Base())
	v.Eu.Cargos = c.Membro.Cargos
	if v.Eu.Cargos == nil {
		v.Eu.Cargos = []int64{}
	}
	if !c.Membro.CastigadoAte.IsZero() && c.Membro.CastigadoAte.After(s.agora()) {
		t := c.Membro.CastigadoAte
		v.Eu.CastigadoAte = &t
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM membros WHERE servidor_id = $1`, servidorID).Scan(&v.TotalMembros)
	return v, err
}

// preencherLeitura põe em cada canal o id da última mensagem e até onde a pessoa leu.
func (s *Servico) preencherLeitura(ctx context.Context, usuarioID int64, ids []int64, canais []Canal) error {
	if len(ids) == 0 {
		return nil
	}
	ultimas := map[int64]int64{}
	linhas, err := s.db.QueryContext(ctx, `
		SELECT c.id, COALESCE((SELECT max(m.id) FROM mensagens m WHERE m.canal_id = c.id), 0), COALESCE(l.ultima_lida, 0)
		FROM canais c LEFT JOIN leituras l ON l.canal_id = c.id AND l.usuario_id = $2
		WHERE c.id = ANY($1)`, pq.Array(ids), usuarioID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	lidas := map[int64]int64{}
	for linhas.Next() {
		var canal, ultima, lida int64
		if err := linhas.Scan(&canal, &ultima, &lida); err != nil {
			return err
		}
		ultimas[canal], lidas[canal] = ultima, lida
	}
	for i := range canais {
		canais[i].UltimaMsgID = ultimas[canais[i].ID]
		canais[i].UltimaLida = lidas[canais[i].ID]
	}
	return linhas.Err()
}

func (s *Servico) RotaVisao(w http.ResponseWriter, r *http.Request) error {
	servidorID, err := id(r, "servidor")
	if err != nil {
		return err
	}
	v, err := s.Visao(r.Context(), servidorID, api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, v)
	return nil
}

// Alterar muda o nome do servidor.
func (s *Servico) Alterar(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarServidor); err != nil {
		return err
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome, err := validarNome(p.Nome, 2, 60, "O nome do servidor")
	if err != nil {
		return err
	}
	var v Servidor
	if err := s.db.QueryRowContext(r.Context(),
		`UPDATE servidores s SET nome = $2 WHERE id = $1 RETURNING `+colunasServidor, servidorID, nome).
		Scan(&v.ID, &v.Nome, &v.DonoID, &v.CriadoEm, &v.Icone); err != nil {
		return err
	}
	v.Icone = urlIcone(v.Icone)
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "servidor_alterado", nome, "")
	s.publicar(r.Context(), eventos.Novo(eventos.ServidorAlterado, servidorID, 0, v))
	api.Responder(w, http.StatusOK, v)
	return nil
}

// Apagar remove o servidor inteiro (só o dono).
func (s *Servico) Apagar(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if !c.Dono() {
		return api.NovoErro(http.StatusForbidden, "Só o dono pode apagar o servidor.")
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM servidores WHERE id = $1`, servidorID); err != nil {
		return err
	}
	s.publicar(r.Context(), eventos.Evento{Tipo: eventos.ServidorApagado, ServidorID: servidorID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Sair deixa o servidor (o dono não pode sair sem apagar).
func (s *Servico) Sair(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if c.Dono() {
		return api.NovoErro(http.StatusBadRequest, "O dono não pode sair. Apague o servidor ou passe a posse antes.")
	}
	return s.removerMembro(w, r, servidorID, c.Membro.UsuarioID, "saiu")
}

func (s *Servico) removerMembro(w http.ResponseWriter, r *http.Request, servidorID, usuarioID int64, motivo string) error {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM membros WHERE servidor_id = $1 AND usuario_id = $2`, servidorID, usuarioID); err != nil {
		return err
	}
	s.publicar(r.Context(), eventos.Evento{Tipo: eventos.MembroSaiu, ServidorID: servidorID, UsuarioID: usuarioID,
		Dados: []byte(`{"usuarioId":` + itoa64(usuarioID) + `,"motivo":"` + motivo + `"}`)})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- leituras usadas por várias rotas ----

func (s *Servico) cargos(ctx context.Context, servidorID int64) ([]Cargo, error) {
	linhas, err := s.db.QueryContext(ctx, `
		SELECT id, servidor_id, nome, cor, permissoes, posicao, padrao, mencionavel
		FROM cargos WHERE servidor_id = $1 ORDER BY posicao DESC, id`, servidorID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []Cargo{}
	for linhas.Next() {
		var c Cargo
		var perm int64
		if err := linhas.Scan(&c.ID, &c.ServidorID, &c.Nome, &c.Cor, &perm, &c.Posicao, &c.Padrao, &c.Mencionavel); err != nil {
			return nil, err
		}
		c.Permissoes = uint64(perm)
		lista = append(lista, c)
	}
	return lista, linhas.Err()
}

func (s *Servico) categorias(ctx context.Context, servidorID int64) ([]Categoria, error) {
	linhas, err := s.db.QueryContext(ctx,
		`SELECT id, servidor_id, nome, posicao FROM categorias WHERE servidor_id = $1 ORDER BY posicao, id`, servidorID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []Categoria{}
	for linhas.Next() {
		var c Categoria
		if err := linhas.Scan(&c.ID, &c.ServidorID, &c.Nome, &c.Posicao); err != nil {
			return nil, err
		}
		lista = append(lista, c)
	}
	return lista, linhas.Err()
}

const colunasCanal = `id, servidor_id, categoria_id, nome, tipo, topico, posicao, modo_lento`

func lerCanal(l interface{ Scan(...any) error }) (Canal, error) {
	var c Canal
	var cat sql.NullInt64
	err := l.Scan(&c.ID, &c.ServidorID, &cat, &c.Nome, &c.Tipo, &c.Topico, &c.Posicao, &c.ModoLento)
	if cat.Valid {
		c.CategoriaID = &cat.Int64
	}
	return c, err
}

func (s *Servico) canais(ctx context.Context, servidorID int64) ([]Canal, error) {
	linhas, err := s.db.QueryContext(ctx,
		`SELECT `+colunasCanal+` FROM canais WHERE servidor_id = $1 ORDER BY posicao, id`, servidorID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []Canal{}
	for linhas.Next() {
		c, err := lerCanal(linhas)
		if err != nil {
			return nil, err
		}
		lista = append(lista, c)
	}
	return lista, linhas.Err()
}
