package servidor

import (
	"fmt"
	"testing"
)

// servidorCom cria um servidor do dono, coloca os outros nele e devolve o id e o canal #geral.
func servidorCom(t *testing.T, dono *cliente, outros ...*cliente) (int64, int64) {
	t.Helper()
	var v visao
	dono.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Servidor de teste"}, &v)
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}
	var conv struct {
		Codigo string `json:"codigo"`
	}
	dono.esperar(201, "POST", fmt.Sprintf("/api/servidores/%d/canais/%d/convites", v.Servidor.ID, geral), map[string]int{}, &conv)
	for _, o := range outros {
		o.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	}
	return v.Servidor.ID, geral
}

func TestFixarMensagens(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	_, geral := servidorCom(t, ana, bia)
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)

	var m struct {
		ID       int64   `json:"id"`
		FixadaEm *string `json:"fixadaEm"`
	}
	bia.esperar(201, "POST", msgs, map[string]string{"conteudo": "regra 1: sem spoiler"}, &m)
	fixar := fmt.Sprintf("%s/%d/fixar", msgs, m.ID)

	bia.esperar(403, "PUT", fixar, nil, nil) // membro comum não gerencia mensagens
	ana.esperar(200, "PUT", fixar, nil, &m)
	if m.FixadaEm == nil {
		t.Fatal("a mensagem deveria voltar com fixadaEm")
	}
	var lista []struct {
		ID int64 `json:"id"`
	}
	bia.esperar(200, "GET", fmt.Sprintf("/api/canais/%d/fixadas", geral), nil, &lista)
	if len(lista) != 1 || lista[0].ID != m.ID {
		t.Fatalf("fixadas: %+v", lista)
	}
	m.FixadaEm = nil
	ana.esperar(200, "DELETE", fixar, nil, &m)
	if m.FixadaEm != nil {
		t.Fatal("desafixar deveria limpar fixadaEm")
	}
	bia.esperar(200, "GET", fmt.Sprintf("/api/canais/%d/fixadas", geral), nil, &lista)
	if len(lista) != 0 {
		t.Fatalf("depois de desafixar a lista deveria ficar vazia: %+v", lista)
	}
}
