package eventos

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type caixa struct {
	mu        sync.Mutex
	recebidos []Evento
}

func (c *caixa) Entregar(_ string, e Evento) {
	c.mu.Lock()
	c.recebidos = append(c.recebidos, e)
	c.mu.Unlock()
}

func (c *caixa) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.recebidos)
}

func TestHubEntregaSoQuemAssinou(t *testing.T) {
	h := NovoHub()
	a, b := &caixa{}, &caixa{}
	h.Assinar("servidor:1", a)
	h.Assinar("servidor:2", b)
	_ = Local{h}.Publicar(context.Background(), "servidor:1", Novo(MensagemCriada, 1, 5, map[string]string{"x": "y"}))
	if a.total() != 1 || b.total() != 0 {
		t.Fatalf("entregas: a=%d b=%d", a.total(), b.total())
	}
	if string(a.recebidos[0].Pronto) != `{"t":"MENSAGEM_CRIADA","s":1,"c":5,"d":{"x":"y"}}` {
		t.Errorf("JSON para o navegador: %s", a.recebidos[0].Pronto)
	}
	h.Cancelar("servidor:1", a)
	_ = Local{h}.Publicar(context.Background(), "servidor:1", Novo(MensagemCriada, 1, 5, nil))
	if a.total() != 1 || h.Assinantes("servidor:1") != 0 {
		t.Error("depois de cancelar não recebe mais")
	}
}

func TestCodificarEscondePermissao(t *testing.T) {
	e := Novo(Digitando, 1, 2, nil)
	e.Exige = 1
	if got := string(e.Codificar()); got != `{"t":"DIGITANDO","s":1,"c":2,"d":null}` {
		t.Errorf("codificado: %s", got)
	}
}

func TestRedisEntreHubs(t *testing.T) {
	endereco := os.Getenv("PROSA_TESTE_REDIS")
	if endereco == "" {
		t.Skip("defina PROSA_TESTE_REDIS")
	}
	o, _ := redis.ParseURL(endereco)
	c := redis.NewClient(o)
	defer c.Close()
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()

	h1, h2 := NovoHub(), NovoHub()
	p1, _ := NovoRedis(ctx, c, h1), NovoRedis(ctx, c, h2)
	quem := &caixa{}
	h2.Assinar("teste-eventos:9", quem)
	if err := h2.Aguardar(ctx, "teste-eventos:9"); err != nil {
		t.Fatal(err)
	}
	_ = p1.Publicar(ctx, "teste-eventos:9", Novo(CanalCriado, 9, 1, nil))
	_ = p1.Publicar(ctx, "teste-eventos:8", Novo(CanalCriado, 8, 1, nil)) // ninguém assinou
	limite := time.Now().Add(3 * time.Second)
	for quem.total() == 0 && time.Now().Before(limite) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if quem.total() != 1 || quem.recebidos[0].ServidorID != 9 {
		t.Fatalf("recebidos: %+v", quem.recebidos)
	}
}
