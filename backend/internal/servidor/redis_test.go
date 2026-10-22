package servidor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/presenca"
)

// Duas instâncias do Prosa dividindo o banco e o Redis: quem está conectado numa recebe
// o que acontece na outra. Precisa de PROSA_TESTE_REDIS (ex.: redis://localhost:6379/15).
func TestDuasInstancias(t *testing.T) {
	endereco := os.Getenv("PROSA_TESTE_REDIS")
	if endereco == "" {
		t.Skip("defina PROSA_TESTE_REDIS para testar várias instâncias")
	}
	db := bancoDeTeste(t)
	opcoes, err := redis.ParseURL(endereco)
	if err != nil {
		t.Fatal(err)
	}
	opcoes.DB = 14 // cada pacote usa um banco do Redis (os testes rodam em paralelo)
	cliente := redis.NewClient(opcoes)
	t.Cleanup(func() { cliente.Close() })
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar)
	if err := cliente.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	subir := func(nome string) string {
		hub := eventos.NovoHub()
		pub := eventos.NovoRedis(ctx, cliente, hub)
		pres := presenca.NovoRedis(ctx, cliente, nome)
		return instancia(t, db, hub, pub, pres, nome).URL
	}
	a, b := subir("a"), subir("b")

	ana := cadastrar(t, a, "ana")
	bia := cadastrar(t, b, "bia")
	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Duas Casas"}, &v)
	sid := v.Servidor.ID
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}

	wa := conectar(t, a, ana.token) // Ana na instância A
	wa.esperar("PRONTO", nil)
	wb := conectar(t, b, bia.token) // Bia na instância B
	wb.esperar("PRONTO", nil)

	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("/api/servidores/%d/canais/%d/convites", sid, geral), map[string]int{}, &conv)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil) // pela instância B
	wb.esperar("SERVIDOR_CRIADO", nil)
	wa.esperar("MEMBRO_ENTROU", nil)
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), fmt.Sprintf(`"usuarioId":%d`, bia.id)) })

	// mensagem enviada pela A chega na conexão da B, e vice-versa
	ana.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", geral), map[string]string{"conteudo": "oi da casa A"}, nil)
	wb.esperar("MENSAGEM_CRIADA", func(e evento) bool { return strings.Contains(string(e.Dados), "casa A") })
	bia.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", geral), map[string]string{"conteudo": "oi da casa B"}, nil)
	wa.esperar("MENSAGEM_CRIADA", func(e evento) bool { return strings.Contains(string(e.Dados), "casa B") })

	// digitando atravessa as instâncias
	wb.mandar("DIGITANDO", map[string]int64{"canalId": geral})
	wa.esperar("DIGITANDO", nil)

	// presença vista pela outra instância: conectar de novo a Bia pela A mostra ela online
	wa2 := conectar(t, a, ana.token)
	p := wa2.esperar("PRONTO", nil)
	if !strings.Contains(string(p.Dados), fmt.Sprintf(`"%d":[`, sid)) || !strings.Contains(string(p.Dados), fmt.Sprint(bia.id)) {
		t.Fatalf("a instância A não viu a Bia online: %s", p.Dados)
	}

	// Bia sai: a Ana (na outra instância) recebe offline
	wb.ws.CloseNow()
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":false`) })
}
