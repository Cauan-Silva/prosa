// Package acesso carrega do banco tudo o que o cálculo de permissões precisa para uma
// pessoa num servidor, e responde "pode?" para as rotas e para o tempo real.
package acesso

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

var (
	ErrNaoMembro  = api.NovoErro(http.StatusNotFound, "Servidor não encontrado ou você não faz parte dele.")
	ErrCanalSumiu = api.NovoErro(http.StatusNotFound, "Canal não encontrado.")
	ErrHierarquia = api.NovoErro(http.StatusForbidden, "Essa pessoa tem um cargo igual ou acima do seu.")
	ErrCargoAcima = api.NovoErro(http.StatusForbidden, "Você só mexe em cargos abaixo do seu cargo mais alto.")
)

// Estrutura é a parte do servidor que vale para todos: dono, cargos, canais e ajustes.
type Estrutura struct {
	ServidorID   int64
	Servidor     permissoes.Servidor
	Posicoes     map[int64]int                      // posição de cada cargo
	Canais       map[int64]string                   // id → tipo ("texto", "voz" ou "fio")
	Pais         map[int64]int64                    // fio → canal pai (de quem ele herda as permissões)
	Sobrescritas map[int64][]permissoes.Sobrescrita // por canal
}

// Contexto é a estrutura do servidor vista por uma pessoa.
type Contexto struct {
	*Estrutura
	Membro permissoes.Membro
	agora  func() time.Time
	fixas  map[int64]permissoes.Permissao // conversas diretas: permissões fixas por canal
}

// PermissoesDiretas é o que cada participante pode numa DM ou grupo.
const PermissoesDiretas = permissoes.VerCanal | permissoes.LerHistorico | permissoes.EnviarMensagens |
	permissoes.AnexarArquivos | permissoes.AdicionarReacoes

// Direto monta o contexto de uma conversa direta: sem servidor, com permissões fixas.
func Direto(canalID, usuarioID int64, perm permissoes.Permissao) *Contexto {
	e := &Estrutura{
		Servidor:     permissoes.Servidor{Cargos: map[int64]permissoes.Permissao{}},
		Posicoes:     map[int64]int{},
		Canais:       map[int64]string{canalID: "texto"},
		Sobrescritas: map[int64][]permissoes.Sobrescrita{},
	}
	c := Novo(e, permissoes.Membro{UsuarioID: usuarioID})
	c.fixas = map[int64]permissoes.Permissao{canalID: perm}
	return c
}

// EhDireto diz se o contexto é de uma conversa direta (sem servidor).
func (c *Contexto) EhDireto() bool { return c.fixas != nil }

type Consulta interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CarregarEstrutura lê dono, cargos, canais e ajustes do servidor.
func CarregarEstrutura(ctx context.Context, db Consulta, servidorID int64) (*Estrutura, error) {
	e := &Estrutura{
		ServidorID:   servidorID,
		Servidor:     permissoes.Servidor{Cargos: map[int64]permissoes.Permissao{}},
		Posicoes:     map[int64]int{},
		Canais:       map[int64]string{},
		Pais:         map[int64]int64{},
		Sobrescritas: map[int64][]permissoes.Sobrescrita{},
	}
	err := db.QueryRowContext(ctx, `SELECT dono_id FROM servidores WHERE id = $1`, servidorID).Scan(&e.Servidor.DonoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoMembro
	}
	if err != nil {
		return nil, err
	}

	linhas, err := db.QueryContext(ctx, `SELECT id, permissoes, posicao, padrao FROM cargos WHERE servidor_id = $1`, servidorID)
	if err != nil {
		return nil, err
	}
	for linhas.Next() {
		var id, perm int64
		var pos int
		var padrao bool
		if err := linhas.Scan(&id, &perm, &pos, &padrao); err != nil {
			linhas.Close()
			return nil, err
		}
		e.Servidor.Cargos[id] = permissoes.Permissao(perm)
		e.Posicoes[id] = pos
		if padrao {
			e.Servidor.CargoTodosID = id
		}
	}
	linhas.Close()

	linhas, err = db.QueryContext(ctx, `SELECT id, tipo, pai_id FROM canais WHERE servidor_id = $1`, servidorID)
	if err != nil {
		return nil, err
	}
	for linhas.Next() {
		var id int64
		var tipo string
		var pai sql.NullInt64
		if err := linhas.Scan(&id, &tipo, &pai); err != nil {
			linhas.Close()
			return nil, err
		}
		e.Canais[id] = tipo
		if pai.Valid {
			e.Pais[id] = pai.Int64
		}
	}
	linhas.Close()

	linhas, err = db.QueryContext(ctx, `
		SELECT p.canal_id, p.tipo, p.alvo_id, p.permitir, p.negar
		FROM permissoes_canal p JOIN canais c ON c.id = p.canal_id
		WHERE c.servidor_id = $1`, servidorID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	for linhas.Next() {
		var canal, alvo, permitir, negar int64
		var tipo string
		if err := linhas.Scan(&canal, &tipo, &alvo, &permitir, &negar); err != nil {
			return nil, err
		}
		e.Sobrescritas[canal] = append(e.Sobrescritas[canal], permissoes.Sobrescrita{
			Tipo: tipo, AlvoID: alvo, Permitir: permissoes.Permissao(permitir), Negar: permissoes.Permissao(negar),
		})
	}
	return e, linhas.Err()
}

// CarregarMembro lê os cargos e o castigo da pessoa no servidor.
func CarregarMembro(ctx context.Context, db Consulta, servidorID, usuarioID int64) (permissoes.Membro, error) {
	m := permissoes.Membro{UsuarioID: usuarioID}
	var castigo sql.NullTime
	var cargos pq.Int64Array
	err := db.QueryRowContext(ctx, `
		SELECT m.castigado_ate,
		       COALESCE(array_agg(mc.cargo_id) FILTER (WHERE mc.cargo_id IS NOT NULL), '{}')
		FROM membros m
		LEFT JOIN membros_cargos mc ON mc.servidor_id = m.servidor_id AND mc.usuario_id = m.usuario_id
		WHERE m.servidor_id = $1 AND m.usuario_id = $2
		GROUP BY m.castigado_ate`, servidorID, usuarioID).Scan(&castigo, &cargos)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNaoMembro
	}
	if err != nil {
		return m, err
	}
	m.Cargos = cargos
	if castigo.Valid {
		m.CastigadoAte = castigo.Time
	}
	return m, nil
}

// Carregar monta o contexto completo (estrutura + membro) direto do banco.
func Carregar(ctx context.Context, db Consulta, servidorID, usuarioID int64) (*Contexto, error) {
	m, err := CarregarMembro(ctx, db, servidorID, usuarioID)
	if err != nil {
		return nil, err
	}
	e, err := CarregarEstrutura(ctx, db, servidorID)
	if err != nil {
		return nil, err
	}
	return Novo(e, m), nil
}

func Novo(e *Estrutura, m permissoes.Membro) *Contexto {
	return &Contexto{Estrutura: e, Membro: m, agora: time.Now}
}

// Base são as permissões no servidor (sem canal).
func (c *Contexto) Base() permissoes.Permissao {
	if c.fixas != nil {
		return 0
	}
	return permissoes.Base(c.Servidor, c.Membro, c.agora())
}

// NoCanal são as permissões num canal (0 se o canal não for deste servidor).
func (c *Contexto) NoCanal(canalID int64) permissoes.Permissao {
	if c.fixas != nil {
		return c.fixas[canalID]
	}
	if _, ok := c.Canais[canalID]; !ok {
		return 0
	}
	if pai, ok := c.Pais[canalID]; ok {
		canalID = pai // um fio tem as permissões do canal de onde saiu
	}
	return permissoes.NoCanal(c.Servidor, c.Membro, c.Sobrescritas[canalID], c.agora())
}

func (c *Contexto) Dono() bool { return c.Membro.UsuarioID == c.Servidor.DonoID }

// Exigir devolve 403 explicando o que falta.
func (c *Contexto) Exigir(p permissoes.Permissao) error {
	if c.Base().Tem(p) {
		return nil
	}
	return falta(p)
}

// ExigirNoCanal devolve 404 se a pessoa nem vê o canal, ou 403 se falta a permissão.
func (c *Contexto) ExigirNoCanal(canalID int64, p permissoes.Permissao) error {
	tem := c.NoCanal(canalID)
	if !tem.Tem(permissoes.VerCanal) {
		return ErrCanalSumiu
	}
	if !tem.Tem(p) {
		return falta(p)
	}
	return nil
}

// PosicaoMaisAlta é a posição do cargo mais alto da pessoa.
func (c *Contexto) PosicaoMaisAlta() int {
	return permissoes.PosicaoMaisAlta(c.Servidor, c.Membro, c.Posicoes)
}

// AcimaDe diz se a pessoa pode moderar outra (expulsar, banir, castigar, mudar cargos).
func (c *Contexto) AcimaDe(outro permissoes.Membro) bool {
	if outro.UsuarioID == c.Servidor.DonoID {
		return false
	}
	return c.PosicaoMaisAlta() > permissoes.PosicaoMaisAlta(c.Servidor, outro, c.Posicoes)
}

// PodeMexerNoCargo diz se o cargo está abaixo do cargo mais alto da pessoa.
func (c *Contexto) PodeMexerNoCargo(cargoID int64) bool {
	return c.Dono() || c.Posicoes[cargoID] < c.PosicaoMaisAlta()
}

func falta(p permissoes.Permissao) error {
	nome := permissoes.Nomes[p]
	if nome == "" {
		return api.NovoErro(http.StatusForbidden, "Você não tem permissão para isso.")
	}
	return api.NovoErro(http.StatusForbidden, "Você não tem a permissão \""+nome+"\" aqui.")
}
