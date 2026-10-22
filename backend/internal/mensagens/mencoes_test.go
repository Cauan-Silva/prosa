package mensagens

import (
	"reflect"
	"testing"
	"time"
)

func TestLerMencoes(t *testing.T) {
	m := LerMencoes("oi <@12> e <@&7>, <@12> de novo, @todos! e-mail x@todos.com não conta")
	if !reflect.DeepEqual(m.Usuarios, []int64{12}) || !reflect.DeepEqual(m.Cargos, []int64{7}) || !m.Todos {
		t.Errorf("menções erradas: %+v", m)
	}
	if LerMencoes("mande para x@todos.com").Todos {
		t.Error("e-mail não é @todos")
	}
	if LerMencoes("@@todos").Todos {
		t.Error("@@todos não é menção")
	}
	if m := LerMencoes("<@0> <@abc> <@99999999999999999999>"); len(m.Usuarios) != 0 {
		t.Errorf("ids inválidos viraram menção: %v", m.Usuarios)
	}
}

func TestLimitadorDeMensagens(t *testing.T) {
	l := &limitador{janela: 5 * time.Second, maximo: 3, vistos: map[int64][]time.Time{}}
	agora := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !l.permitir(1, agora) {
			t.Fatal("deveria permitir as primeiras")
		}
	}
	if l.permitir(1, agora) {
		t.Error("a quarta na mesma janela deveria ser barrada")
	}
	if !l.permitir(2, agora) {
		t.Error("outra pessoa não é afetada")
	}
	if !l.permitir(1, agora.Add(6*time.Second)) {
		t.Error("depois da janela libera de novo")
	}
}
