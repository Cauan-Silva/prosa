package presenca

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestLocalContaAbas(t *testing.T) {
	ctx := context.Background()
	l := NovoLocal()
	if p, _ := l.Conectou(ctx, 1); !p {
		t.Error("primeira aba deixa online")
	}
	if p, _ := l.Conectou(ctx, 1); p {
		t.Error("segunda aba não é 'primeira'")
	}
	if u, _ := l.Desconectou(ctx, 1); u {
		t.Error("ainda tem uma aba aberta")
	}
	on, _ := l.Online(ctx, []int64{1, 2})
	if !on[1] || on[2] {
		t.Errorf("online errado: %v", on)
	}
	if u, _ := l.Desconectou(ctx, 1); !u {
		t.Error("fechou a última aba: offline")
	}
}

func redisDeTeste(t *testing.T) *redis.Client {
	t.Helper()
	endereco := os.Getenv("PROSA_TESTE_REDIS")
	if endereco == "" {
		t.Skip("defina PROSA_TESTE_REDIS")
	}
	o, err := redis.ParseURL(endereco)
	if err != nil {
		t.Fatal(err)
	}
	o.DB = 12 // cada pacote usa um banco do Redis (os testes rodam em paralelo)
	c := redis.NewClient(o)
	c.FlushDB(context.Background())
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRedisEntreInstancias(t *testing.T) {
	c := redisDeTeste(t)
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	a := NovoRedis(ctx, c, "a")
	ctxB, derrubarB := context.WithCancel(ctx)
	b := NovoRedis(ctxB, c, "b")

	if p, _ := a.Conectou(ctx, 7); !p {
		t.Error("primeira conexão (instância A)")
	}
	if p, _ := b.Conectou(ctx, 7); p {
		t.Error("na instância B já estava online pela A")
	}
	if u, _ := a.Desconectou(ctx, 7); u {
		t.Error("ainda conectado pela B")
	}
	on, _ := a.Online(ctx, []int64{7, 8})
	if !on[7] || on[8] {
		t.Errorf("online errado: %v", on)
	}

	// a instância B "cai": a chave dela some e a pessoa deixa de contar
	derrubarB()
	c.Del(ctx, chaveInstancia("b"))
	on, _ = a.Online(ctx, []int64{7})
	if on[7] {
		t.Error("quem estava só numa instância que caiu não está online")
	}
}
