package permissoes

import (
	"testing"
	"time"
)

var agora = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

const (
	dono     = 1
	todos    = 100
	moderad  = 101
	monitor  = 102
	silencia = 103
)

func servidorTeste() Servidor {
	return Servidor{
		DonoID:       dono,
		CargoTodosID: todos,
		Cargos: map[int64]Permissao{
			todos:    Padrao,
			moderad:  GerenciarMensagens | Expulsar | Castigar,
			monitor:  GerenciarCanais,
			silencia: 0,
		},
	}
}

func TestDonoPodeTudo(t *testing.T) {
	s := servidorTeste()
	if Base(s, Membro{UsuarioID: dono}, agora) != Todas {
		t.Error("o dono deveria poder tudo")
	}
	negaTudo := []Sobrescrita{{Tipo: "membro", AlvoID: dono, Negar: Todas}}
	if NoCanal(s, Membro{UsuarioID: dono}, negaTudo, agora) != Todas {
		t.Error("nem um ajuste de canal tira poder do dono")
	}
}

func TestMembroSomaOsCargos(t *testing.T) {
	s := servidorTeste()
	p := Base(s, Membro{UsuarioID: 2, Cargos: []int64{moderad, monitor}}, agora)
	for _, esperada := range []Permissao{EnviarMensagens, GerenciarMensagens, Expulsar, GerenciarCanais} {
		if !p.Tem(esperada) {
			t.Errorf("deveria ter %s", Nomes[esperada])
		}
	}
	if p.Tem(Banir) {
		t.Error("ninguém deu Banir")
	}
}

func TestAdministradorPodeTudo(t *testing.T) {
	s := servidorTeste()
	s.Cargos[monitor] |= Administrador
	m := Membro{UsuarioID: 2, Cargos: []int64{monitor}}
	if Base(s, m, agora) != Todas {
		t.Error("administrador deveria poder tudo")
	}
	if NoCanal(s, m, []Sobrescrita{{Tipo: "cargo", AlvoID: todos, Negar: VerCanal}}, agora) != Todas {
		t.Error("administrador ignora os ajustes do canal")
	}
}

func TestCanalPrivadoSoParaUmCargo(t *testing.T) {
	s := servidorTeste()
	canal := []Sobrescrita{
		{Tipo: "cargo", AlvoID: todos, Negar: VerCanal},
		{Tipo: "cargo", AlvoID: moderad, Permitir: VerCanal},
	}
	comum := Membro{UsuarioID: 2}
	mod := Membro{UsuarioID: 3, Cargos: []int64{moderad}}
	if NoCanal(s, comum, canal, agora) != 0 {
		t.Error("quem não vê o canal não tem permissão nenhuma nele")
	}
	if p := NoCanal(s, mod, canal, agora); !p.Tem(VerCanal | EnviarMensagens) {
		t.Errorf("o moderador deveria ver e escrever: %b", p)
	}
}

func TestOrdemDasSobrescritas(t *testing.T) {
	s := servidorTeste()
	m := Membro{UsuarioID: 2, Cargos: []int64{moderad, silencia}}

	// Um cargo nega, outro permite: a permissão vence (as permissões dos cargos vêm depois das negações).
	canal := []Sobrescrita{
		{Tipo: "cargo", AlvoID: silencia, Negar: EnviarMensagens},
		{Tipo: "cargo", AlvoID: moderad, Permitir: EnviarMensagens},
	}
	if !NoCanal(s, m, canal, agora).Tem(EnviarMensagens) {
		t.Error("entre cargos, permitir vence negar")
	}

	// O ajuste da própria pessoa vence o dos cargos.
	canal = append(canal, Sobrescrita{Tipo: "membro", AlvoID: 2, Negar: EnviarMensagens})
	if NoCanal(s, m, canal, agora).Tem(EnviarMensagens) {
		t.Error("o ajuste do membro deveria vencer")
	}

	// @todos permite, mas um cargo nega: o cargo vence o @todos.
	canal = []Sobrescrita{
		{Tipo: "cargo", AlvoID: todos, Permitir: MencionarTodos},
		{Tipo: "cargo", AlvoID: silencia, Negar: MencionarTodos},
	}
	if NoCanal(s, m, canal, agora).Tem(MencionarTodos) {
		t.Error("o cargo vem depois do @todos")
	}
}

func TestCanalSoLeitura(t *testing.T) {
	s := servidorTeste()
	avisos := []Sobrescrita{{Tipo: "cargo", AlvoID: todos, Negar: EnviarMensagens}}
	p := NoCanal(s, Membro{UsuarioID: 2}, avisos, agora)
	if !p.Tem(VerCanal|LerHistorico) || p.Tem(EnviarMensagens) || p.Tem(AnexarArquivos) {
		t.Errorf("canal de avisos: lê, mas não escreve nem anexa: %b", p)
	}
}

func TestCastigoDeixaSoLer(t *testing.T) {
	s := servidorTeste()
	m := Membro{UsuarioID: 2, Cargos: []int64{moderad}, CastigadoAte: agora.Add(10 * time.Minute)}
	if p := Base(s, m, agora); p != SoLeitura {
		t.Errorf("de castigo deveria só ler: %b", p)
	}
	canal := []Sobrescrita{{Tipo: "membro", AlvoID: 2, Permitir: EnviarMensagens}}
	if NoCanal(s, m, canal, agora).Tem(EnviarMensagens) {
		t.Error("nem um ajuste do canal tira alguém do castigo")
	}
	if !Base(s, m, agora.Add(11*time.Minute)).Tem(EnviarMensagens) {
		t.Error("o castigo acaba sozinho")
	}
}

func TestVozDependeDeConectar(t *testing.T) {
	s := servidorTeste()
	canal := []Sobrescrita{{Tipo: "cargo", AlvoID: todos, Negar: ConectarVoz}}
	if p := NoCanal(s, Membro{UsuarioID: 2}, canal, agora); p.Tem(Falar) || p.Tem(Video) {
		t.Error("sem entrar na voz não dá para falar nem ligar câmera")
	}
}

func TestHierarquia(t *testing.T) {
	s := servidorTeste()
	pos := map[int64]int{moderad: 5, monitor: 3}
	if PosicaoMaisAlta(s, Membro{UsuarioID: 2, Cargos: []int64{monitor, moderad}}, pos) != 5 {
		t.Error("vale o cargo mais alto")
	}
	if PosicaoMaisAlta(s, Membro{UsuarioID: 2}, pos) != 0 {
		t.Error("só @todos é posição 0")
	}
	if PosicaoMaisAlta(s, Membro{UsuarioID: dono}, pos) <= 5 {
		t.Error("o dono fica acima de todos")
	}
}

func TestTodasCobreTodasAsPermissoes(t *testing.T) {
	if len(Nomes) != Todas.Quantas() {
		t.Errorf("há %d nomes para %d permissões", len(Nomes), Todas.Quantas())
	}
	for p := range Nomes {
		if !Todas.Tem(p) {
			t.Errorf("%s fora de Todas", Nomes[p])
		}
	}
}
