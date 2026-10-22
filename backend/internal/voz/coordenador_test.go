package voz

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestCoordenadorRedis(t *testing.T) {
	endereco := os.Getenv("PROSA_TESTE_REDIS")
	if endereco == "" {
		t.Skip("defina PROSA_TESTE_REDIS")
	}
	o, _ := redis.ParseURL(endereco)
	o.DB = 13 // cada pacote usa um banco do Redis (os testes rodam em paralelo)
	c := redis.NewClient(o)
	defer c.Close()
	ctx := context.Background()
	c.FlushDB(ctx)
	c.Set(ctx, chaveInstancia("a"), "1", 0)
	a := Redis{Cliente: c, Instancia: "a"}
	b := Redis{Cliente: c, Instancia: "b"}

	if nossa, _, _ := a.Reservar(ctx, 10, "http://a"); !nossa {
		t.Fatal("primeira instância fica com a sala")
	}
	nossa, dono, _ := b.Reservar(ctx, 10, "http://b")
	if nossa || dono != "http://a" {
		t.Fatalf("a outra instância deve ser mandada para a dona: %v %q", nossa, dono)
	}
	if d, _ := b.Dono(ctx, 10); d != "http://a" {
		t.Errorf("dono: %q", d)
	}
	b.Liberar(ctx, 10, "http://b") // quem não é dono não libera
	if d, _ := b.Dono(ctx, 10); d != "http://a" {
		t.Error("liberou a sala de outra instância")
	}
	a.Liberar(ctx, 10, "http://a")
	if nossa, _, _ := b.Reservar(ctx, 10, "http://b"); !nossa {
		t.Error("sala liberada pode ir para outra instância")
	}

	// estado: só conta o de instâncias vivas
	_ = a.GravarEstado(ctx, 1, 10, []Estado{{UsuarioID: 5, Nome: "Ana"}})
	_ = b.GravarEstado(ctx, 1, 11, []Estado{{UsuarioID: 6, Nome: "Bia"}}) // "b" não tem chave de vida
	estado, err := a.LerEstado(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(estado["1"]["10"]) != 1 || estado["1"]["11"] != nil {
		t.Errorf("estado errado: %+v", estado)
	}
	_ = a.GravarEstado(ctx, 1, 10, nil)
	if estado, _ = a.LerEstado(ctx, []int64{1}); len(estado) != 0 {
		t.Errorf("sala vazia deveria sumir: %+v", estado)
	}
}

func TestCoordenadorLocal(t *testing.T) {
	ctx := context.Background()
	l := NovoLocal()
	if nossa, _, _ := l.Reservar(ctx, 1, ""); !nossa {
		t.Error("uma instância só: a sala é sempre daqui")
	}
	_ = l.GravarEstado(ctx, 2, 3, []Estado{{UsuarioID: 1}})
	e, _ := l.LerEstado(ctx, []int64{2, 9})
	if len(e) != 1 || len(e["2"]["3"]) != 1 {
		t.Errorf("estado: %+v", e)
	}
}
