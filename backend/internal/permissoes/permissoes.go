// Package permissoes calcula o que cada pessoa pode fazer num servidor e em cada canal.
//
// As permissões são bits num inteiro de 64 bits. Um membro soma (OU) as permissões de
// todos os seus cargos, incluindo o cargo padrão "@todos". Em cada canal, ajustes
// (sobrescritas) podem permitir ou negar bits para um cargo ou para uma pessoa:
//
//  1. @todos do canal:       nega, depois permite
//  2. cargos do membro:      nega a soma das negações, depois permite a soma das permissões
//  3. o próprio membro:      nega, depois permite
//
// O dono do servidor e quem tem ADMINISTRADOR podem tudo. Quem está de castigo só lê.
// Quem não pode ver o canal não tem nenhuma outra permissão nele.
package permissoes

import (
	"math/bits"
	"time"
)

type Permissao uint64

const (
	VerCanal Permissao = 1 << iota
	LerHistorico
	EnviarMensagens
	GerenciarMensagens // apagar mensagens dos outros
	AnexarArquivos
	AdicionarReacoes
	MencionarTodos
	ConectarVoz
	Falar
	Video // câmera e tela
	SilenciarNaVoz
	CriarConvite
	AlterarApelido
	GerenciarApelidos
	GerenciarCanais
	GerenciarCargos
	Expulsar
	Banir
	Castigar
	VerAuditoria
	GerenciarServidor
	Administrador
)

// Todas são todas as permissões existentes.
const Todas = Administrador<<1 - 1

// Padrao é o que o cargo @todos recebe num servidor novo.
const Padrao = VerCanal | LerHistorico | EnviarMensagens | AnexarArquivos | AdicionarReacoes |
	ConectarVoz | Falar | Video | CriarConvite | AlterarApelido

// SoLeitura é o que sobra para quem está de castigo.
const SoLeitura = VerCanal | LerHistorico

// Nomes legíveis (para a tela de cargos e para as mensagens de erro).
var Nomes = map[Permissao]string{
	VerCanal:           "Ver canais",
	LerHistorico:       "Ler mensagens antigas",
	EnviarMensagens:    "Enviar mensagens",
	GerenciarMensagens: "Gerenciar mensagens",
	AnexarArquivos:     "Anexar arquivos",
	AdicionarReacoes:   "Reagir",
	MencionarTodos:     "Mencionar @todos",
	ConectarVoz:        "Entrar na voz",
	Falar:              "Falar",
	Video:              "Câmera e tela",
	SilenciarNaVoz:     "Silenciar na voz",
	CriarConvite:       "Criar convites",
	AlterarApelido:     "Mudar o próprio apelido",
	GerenciarApelidos:  "Mudar apelidos",
	GerenciarCanais:    "Gerenciar canais",
	GerenciarCargos:    "Gerenciar cargos",
	Expulsar:           "Expulsar",
	Banir:              "Banir",
	Castigar:           "Castigar",
	VerAuditoria:       "Ver registro de auditoria",
	GerenciarServidor:  "Gerenciar servidor",
	Administrador:      "Administrador",
}

func (p Permissao) Tem(alvo Permissao) bool { return p&alvo == alvo }

// Quantas permissões estão ligadas (para mostrar na tela).
func (p Permissao) Quantas() int { return bits.OnesCount64(uint64(p & Todas)) }

// Sobrescrita é um ajuste de um canal para um cargo ou para uma pessoa.
type Sobrescrita struct {
	Tipo     string // "cargo" ou "membro"
	AlvoID   int64
	Permitir Permissao
	Negar    Permissao
}

// Membro é o que o cálculo precisa saber sobre a pessoa.
type Membro struct {
	UsuarioID    int64
	Cargos       []int64 // ids dos cargos (sem contar o @todos)
	CastigadoAte time.Time
}

// Servidor é o que o cálculo precisa saber sobre o servidor.
type Servidor struct {
	DonoID       int64
	CargoTodosID int64
	Cargos       map[int64]Permissao // permissões de cada cargo, incluindo o @todos
}

// Base calcula as permissões do membro no servidor (sem olhar canal).
func Base(s Servidor, m Membro, agora time.Time) Permissao {
	if m.UsuarioID == s.DonoID {
		return Todas
	}
	p := s.Cargos[s.CargoTodosID]
	for _, c := range m.Cargos {
		p |= s.Cargos[c]
	}
	if p.Tem(Administrador) {
		return Todas
	}
	if agora.Before(m.CastigadoAte) {
		p &= SoLeitura
	}
	return p & Todas
}

// NoCanal aplica as sobrescritas do canal sobre as permissões base.
func NoCanal(s Servidor, m Membro, sobrescritas []Sobrescrita, agora time.Time) Permissao {
	base := Base(s, m, agora)
	if base == Todas {
		return Todas
	}
	p := base

	cargos := make(map[int64]bool, len(m.Cargos))
	for _, c := range m.Cargos {
		cargos[c] = true
	}
	var negarCargos, permitirCargos Permissao
	var membro *Sobrescrita
	for i := range sobrescritas {
		o := &sobrescritas[i]
		switch {
		case o.Tipo == "cargo" && o.AlvoID == s.CargoTodosID:
			p = (p &^ o.Negar) | o.Permitir
		case o.Tipo == "cargo" && cargos[o.AlvoID]:
			negarCargos |= o.Negar
			permitirCargos |= o.Permitir
		case o.Tipo == "membro" && o.AlvoID == m.UsuarioID:
			membro = o
		}
	}
	p = (p &^ negarCargos) | permitirCargos
	if membro != nil {
		p = (p &^ membro.Negar) | membro.Permitir
	}

	// Castigo vale também contra permissões dadas pelo canal.
	if agora.Before(m.CastigadoAte) {
		p &= SoLeitura
	}
	if !p.Tem(VerCanal) {
		return 0
	}
	// Sem enviar mensagens, também não dá para anexar nem mencionar todos.
	if !p.Tem(EnviarMensagens) {
		p &^= AnexarArquivos | MencionarTodos
	}
	// Sem entrar na voz, nada de falar ou vídeo.
	if !p.Tem(ConectarVoz) {
		p &^= Falar | Video | SilenciarNaVoz
	}
	return p & Todas
}

// Hierarquia: cada cargo tem uma posição; quanto maior, mais acima.

// PosicaoMaisAlta devolve a posição do cargo mais alto do membro (0 = só @todos).
// O dono fica acima de tudo.
func PosicaoMaisAlta(s Servidor, m Membro, posicoes map[int64]int) int {
	if m.UsuarioID == s.DonoID {
		return 1 << 30
	}
	maior := 0
	for _, c := range m.Cargos {
		if p := posicoes[c]; p > maior {
			maior = p
		}
	}
	return maior
}
