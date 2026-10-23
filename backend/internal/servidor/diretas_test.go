package servidor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/Cauan-Silva/prosa/internal/eventos"
)

type listaAmigos struct {
	Amigos     []struct{ ID int64 } `json:"amigos"`
	Recebidos  []struct{ ID int64 } `json:"recebidos"`
	Enviados   []struct{ ID int64 } `json:"enviados"`
	Bloqueados []struct{ ID int64 } `json:"bloqueados"`
}

type conversa struct {
	ID            int64  `json:"id"`
	Tipo          string `json:"tipo"`
	Permissoes    uint64 `json:"permissoes"`
	Participantes []struct {
		ID int64 `json:"id"`
	} `json:"participantes"`
}

func TestAmigosEConversasDiretas(t *testing.T) {
	srv, hub := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	caio := cadastrar(t, srv.URL, "caio")

	// pedido de amizade pelo @nome e aceite
	esp := &espiao{}
	hub.Assinar(eventos.TopicoUsuario(bia.id), esp)
	ana.esperar(404, "POST", "/api/amigos", map[string]string{"nomeUsuario": "ninguem"}, nil)
	ana.esperar(400, "POST", "/api/amigos", map[string]string{"nomeUsuario": "@ana"}, nil)
	ana.esperar(200, "POST", "/api/amigos", map[string]string{"nomeUsuario": "@BIA"}, nil)
	var lb listaAmigos
	bia.esperar(200, "GET", "/api/amigos", nil, &lb)
	if len(lb.Recebidos) != 1 || lb.Recebidos[0].ID != ana.id {
		t.Fatalf("Bia deveria ter o pedido da Ana: %+v", lb)
	}
	bia.esperar(200, "PUT", fmt.Sprintf("/api/amigos/%d", ana.id), nil, nil)
	var la listaAmigos
	ana.esperar(200, "GET", "/api/amigos", nil, &la)
	if len(la.Amigos) != 1 || la.Amigos[0].ID != bia.id {
		t.Fatalf("Ana e Bia deveriam ser amigas: %+v", la)
	}
	if !contem(esp.tipos(), "AMIGO_ALTERADO") {
		t.Errorf("Bia deveria receber AMIGO_ALTERADO, recebeu %v", esp.tipos())
	}

	// DM: abrir duas vezes devolve a mesma; a Bia só vê depois da primeira mensagem
	var dm, dm2 conversa
	ana.esperar(200, "POST", "/api/conversas", map[string][]int64{"usuarios": {bia.id}}, &dm)
	ana.esperar(200, "POST", "/api/conversas", map[string][]int64{"usuarios": {bia.id}}, &dm2)
	if dm.ID == 0 || dm.ID != dm2.ID || dm.Tipo != "dm" || len(dm.Participantes) != 2 {
		t.Fatalf("DM errada: %+v / %+v", dm, dm2)
	}
	var cb []conversa
	bia.esperar(200, "GET", "/api/conversas", nil, &cb)
	if len(cb) != 0 {
		t.Fatalf("a DM só deveria aparecer para a Bia com mensagem: %+v", cb)
	}
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", dm.ID)
	ana.esperar(201, "POST", msgs, map[string]string{"conteudo": "oi <@" + fmt.Sprint(bia.id) + ">, tudo bem?"}, nil)
	bia.esperar(200, "GET", "/api/conversas", nil, &cb)
	if len(cb) != 1 || cb[0].ID != dm.ID {
		t.Fatalf("depois da mensagem a Bia deveria ver a DM: %+v", cb)
	}
	if !contem(esp.tipos(), "MENSAGEM_CRIADA") {
		t.Errorf("a mensagem deveria chegar no tópico da Bia: %v", esp.tipos())
	}
	var lista []struct {
		Conteudo string  `json:"conteudo"`
		Mencoes  []int64 `json:"mencoes"`
	}
	bia.esperar(200, "GET", msgs, nil, &lista)
	if len(lista) != 1 || len(lista[0].Mencoes) != 1 || lista[0].Mencoes[0] != bia.id {
		t.Fatalf("mensagens da DM: %+v", lista)
	}
	// quem não participa não vê nada
	caio.esperar(404, "GET", msgs, nil, nil)
	caio.esperar(404, "POST", msgs, map[string]string{"conteudo": "intruso"}, nil)
	// sem amizade nem servidor em comum, não abre DM
	caio.esperar(403, "POST", "/api/conversas", map[string][]int64{"usuarios": {ana.id}}, nil)

	// anexo numa DM (sem servidor)
	corpo := &bytes.Buffer{}
	mp := multipart.NewWriter(corpo)
	parte, _ := mp.CreateFormFile("arquivo", "foto.txt")
	parte.Write([]byte("um arquivo da DM"))
	mp.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/canais/%d/anexos", srv.URL, dm.ID), corpo)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+bia.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var anexo struct {
		ID       int64  `json:"id"`
		Endereco string `json:"endereco"`
	}
	json.NewDecoder(resp.Body).Decode(&anexo)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("anexo na DM: status %d", resp.StatusCode)
	}
	bia.esperar(201, "POST", msgs, map[string]any{"conteudo": "", "anexos": []int64{anexo.ID}}, nil)
	if s := baixar(t, srv.URL+anexo.Endereco, ana.token); s != 200 {
		t.Errorf("Ana deveria baixar o anexo da DM, status %d", s)
	}
	if s := baixar(t, srv.URL+anexo.Endereco, caio.token); s != 404 {
		t.Errorf("Caio não deveria baixar o anexo da DM, status %d", s)
	}

	// grupo: só com amigos
	ana.esperar(403, "POST", "/api/conversas", map[string]any{"usuarios": []int64{bia.id, caio.id}, "nome": "trio"}, nil)
	caio.esperar(200, "POST", "/api/amigos", map[string]string{"nomeUsuario": "ana"}, nil)
	ana.esperar(200, "PUT", fmt.Sprintf("/api/amigos/%d", caio.id), nil, nil)
	var grupo conversa
	ana.esperar(201, "POST", "/api/conversas", map[string]any{"usuarios": []int64{bia.id, caio.id}, "nome": "trio"}, &grupo)
	if grupo.Tipo != "grupo" || len(grupo.Participantes) != 3 {
		t.Fatalf("grupo errado: %+v", grupo)
	}
	caio.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", grupo.ID), map[string]string{"conteudo": "oi grupo"}, nil)
	bia.esperar(403, "DELETE", fmt.Sprintf("/api/conversas/%d/membros/%d", grupo.ID, caio.id), nil, nil)  // só a dona tira
	caio.esperar(204, "DELETE", fmt.Sprintf("/api/conversas/%d/membros/%d", grupo.ID, caio.id), nil, nil) // sair
	caio.esperar(404, "GET", fmt.Sprintf("/api/canais/%d/mensagens", grupo.ID), nil, nil)

	// bloqueio: desfaz a amizade e a DM fica só para ler
	bia.esperar(204, "PUT", fmt.Sprintf("/api/bloqueios/%d", ana.id), nil, nil)
	ana.esperar(403, "POST", msgs, map[string]string{"conteudo": "volta!"}, nil)
	ana.esperar(200, "GET", msgs, nil, nil) // ler continua podendo
	ana.esperar(403, "POST", "/api/amigos", map[string]string{"nomeUsuario": "bia"}, nil)
	ana.esperar(200, "GET", "/api/amigos", nil, &la)
	if len(la.Amigos) != 1 { // sobrou o Caio
		t.Fatalf("depois do bloqueio a Ana não deveria ter a Bia como amiga: %+v", la)
	}
	bia.esperar(200, "GET", "/api/amigos", nil, &lb)
	if len(lb.Bloqueados) != 1 {
		t.Fatalf("Bia deveria ver a Ana nos bloqueados: %+v", lb)
	}
	bia.esperar(204, "DELETE", fmt.Sprintf("/api/bloqueios/%d", ana.id), nil, nil)
	ana.esperar(201, "POST", msgs, map[string]string{"conteudo": "voltou"}, nil)

	// perfil: visível por causa da DM, com a relação
	var perfil struct {
		Relacao string `json:"relacao"`
	}
	ana.esperar(200, "GET", fmt.Sprintf("/api/usuarios/%d", caio.id), nil, &perfil)
	if perfil.Relacao != "amigo" {
		t.Errorf("relação com o Caio deveria ser amigo, veio %q", perfil.Relacao)
	}
}

func contem(lista []string, v string) bool {
	for _, x := range lista {
		if x == v {
			return true
		}
	}
	return false
}
