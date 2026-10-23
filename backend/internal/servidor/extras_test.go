package servidor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
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

func enviarEmoji(t *testing.T, base string, c *cliente, sid int64, nome string, conteudo []byte) (int, int64) {
	t.Helper()
	corpo := &bytes.Buffer{}
	mp := multipart.NewWriter(corpo)
	mp.WriteField("nome", nome)
	parte, _ := mp.CreateFormFile("arquivo", "emoji.png")
	parte.Write(conteudo)
	mp.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/servidores/%d/emojis", base, sid), corpo)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var e struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	return resp.StatusCode, e.ID
}

// PNG de 1x1 pixel.
var png1x1, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

func TestEmojisDoServidor(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	sid, geral := servidorCom(t, ana, bia)

	if s, _ := enviarEmoji(t, srv.URL, bia, sid, "festa", png1x1); s != 403 {
		t.Fatalf("membro comum não deveria criar emoji: %d", s)
	}
	if s, _ := enviarEmoji(t, srv.URL, ana, sid, "com espaço", png1x1); s != 400 {
		t.Fatalf("nome inválido deveria dar 400: %d", s)
	}
	if s, _ := enviarEmoji(t, srv.URL, ana, sid, "texto", []byte("isto não é imagem")); s != 400 {
		t.Fatalf("arquivo que não é imagem deveria dar 400: %d", s)
	}
	s, id := enviarEmoji(t, srv.URL, ana, sid, "festa", png1x1)
	if s != 201 || id == 0 {
		t.Fatalf("criar emoji: %d", s)
	}
	if s, _ := enviarEmoji(t, srv.URL, ana, sid, "FESTA", png1x1); s != 409 {
		t.Fatalf("nome repetido deveria dar 409: %d", s)
	}
	var v struct {
		Emojis []struct {
			ID   int64  `json:"id"`
			Nome string `json:"nome"`
			URL  string `json:"url"`
		} `json:"emojis"`
	}
	bia.esperar(200, "GET", fmt.Sprintf("/api/servidores/%d", sid), nil, &v)
	if len(v.Emojis) != 1 || v.Emojis[0].Nome != "festa" {
		t.Fatalf("visão deveria trazer o emoji: %+v", v.Emojis)
	}
	// a imagem é pública
	resp, err := http.Get(srv.URL + v.Emojis[0].URL)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("imagem do emoji: %v %v", err, resp)
	}
	resp.Body.Close()
	// reação com o emoji do servidor
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)
	var m struct {
		ID int64 `json:"id"`
	}
	bia.esperar(201, "POST", msgs, map[string]string{"conteudo": fmt.Sprintf("bora <:festa:%d>", id)}, &m)
	bia.esperar(204, "PUT", fmt.Sprintf("%s/%d/reacoes/c:%d", msgs, m.ID, id), nil, nil)
	bia.esperar(400, "PUT", fmt.Sprintf("%s/%d/reacoes/c:999999", msgs, m.ID), nil, nil)
	// renomear e apagar
	ana.esperar(200, "PATCH", fmt.Sprintf("/api/servidores/%d/emojis/%d", sid, id), map[string]string{"nome": "comemora"}, nil)
	ana.esperar(204, "DELETE", fmt.Sprintf("/api/servidores/%d/emojis/%d", sid, id), nil, nil)
	if resp, _ := http.Get(srv.URL + v.Emojis[0].URL); resp == nil || resp.StatusCode != 404 {
		t.Fatal("emoji apagado deveria sumir")
	}
}

func TestEnquetes(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	_, geral := servidorCom(t, ana, bia)
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)

	ana.esperar(400, "POST", msgs, map[string]any{"enquete": map[string]any{"pergunta": "Só uma?", "opcoes": []string{"sim"}}}, nil)
	ana.esperar(400, "POST", msgs, map[string]any{"enquete": map[string]any{"pergunta": "Repetida", "opcoes": []string{"a", "A"}}}, nil)
	type enquete struct {
		Pergunta string `json:"pergunta"`
		Opcoes   []struct {
			Texto string  `json:"texto"`
			Votos []int64 `json:"votos"`
		} `json:"opcoes"`
		Encerrada bool `json:"encerrada"`
	}
	var m struct {
		ID      int64    `json:"id"`
		Enquete *enquete `json:"enquete"`
	}
	ana.esperar(201, "POST", msgs, map[string]any{"enquete": map[string]any{
		"pergunta": "Pizza ou hambúrguer?", "opcoes": []string{"Pizza", "Hambúrguer", " "}, "horas": 24}}, &m)
	if m.Enquete == nil || len(m.Enquete.Opcoes) != 2 {
		t.Fatalf("enquete criada errada: %+v", m.Enquete)
	}
	votos := fmt.Sprintf("%s/%d/votos", msgs, m.ID)
	bia.esperar(400, "PUT", votos, map[string][]int{"opcoes": {0, 1}}, nil) // só uma
	bia.esperar(400, "PUT", votos, map[string][]int{"opcoes": {5}}, nil)
	bia.esperar(200, "PUT", votos, map[string][]int{"opcoes": {1}}, nil)
	ana.esperar(200, "PUT", votos, map[string][]int{"opcoes": {1}}, nil)
	bia.esperar(200, "PUT", votos, map[string][]int{"opcoes": {0}}, &m) // trocou o voto
	if len(m.Enquete.Opcoes[0].Votos) != 1 || len(m.Enquete.Opcoes[1].Votos) != 1 {
		t.Fatalf("votos: %+v", m.Enquete.Opcoes)
	}
	bia.esperar(403, "POST", fmt.Sprintf("%s/%d/encerrar", msgs, m.ID), nil, nil) // não é dela
	ana.esperar(200, "POST", fmt.Sprintf("%s/%d/encerrar", msgs, m.ID), nil, &m)
	if !m.Enquete.Encerrada {
		t.Fatal("a enquete deveria estar encerrada")
	}
	bia.esperar(400, "PUT", votos, map[string][]int{"opcoes": {1}}, nil)
	// aparece na listagem do canal
	var lista []struct {
		Enquete *enquete `json:"enquete"`
	}
	bia.esperar(200, "GET", msgs, nil, &lista)
	if len(lista) != 1 || lista[0].Enquete == nil || lista[0].Enquete.Pergunta != "Pizza ou hambúrguer?" {
		t.Fatalf("listagem sem a enquete: %+v", lista)
	}
}

func TestFios(t *testing.T) {
	srv, hub := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	sid, geral := servidorCom(t, ana, bia)
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)
	esp := &espiao{}
	hub.Assinar(eventos.TopicoServidor(sid), esp)

	var origem struct {
		ID int64 `json:"id"`
	}
	ana.esperar(201, "POST", msgs, map[string]string{"conteudo": "Alguém resolveu o exercício 4 da lista de cálculo?"}, &origem)
	type fio struct {
		ID         int64  `json:"id"`
		Nome       string `json:"nome"`
		CanalPaiID int64  `json:"canalPaiId"`
		Total      int    `json:"total"`
	}
	var f, f2 fio
	bia.esperar(201, "POST", fmt.Sprintf("%s/%d/fio", msgs, origem.ID), map[string]string{}, &f)
	if f.ID == 0 || f.CanalPaiID != geral || f.Nome != "Alguém resolveu o exercício 4 da lista…" {
		t.Fatalf("fio criado errado: %+v", f)
	}
	ana.esperar(200, "POST", fmt.Sprintf("%s/%d/fio", msgs, origem.ID), map[string]string{"nome": "outro"}, &f2) // já existe
	if f2.ID != f.ID {
		t.Fatalf("a mesma mensagem não deveria ter dois fios: %d e %d", f.ID, f2.ID)
	}
	fioMsgs := fmt.Sprintf("/api/canais/%d/mensagens", f.ID)
	ana.esperar(201, "POST", fioMsgs, map[string]string{"conteudo": "eu fiz! dá 3/2"}, nil)
	bia.esperar(201, "POST", fioMsgs, map[string]string{"conteudo": "valeu!"}, nil)
	if !contem(esp.tipos(), "FIO_ATUALIZADO") {
		t.Errorf("deveria publicar FIO_ATUALIZADO: %v", esp.tipos())
	}
	// o resumo aparece na mensagem de origem
	var lista []struct {
		ID  int64 `json:"id"`
		Fio *fio  `json:"fio"`
	}
	bia.esperar(200, "GET", msgs, nil, &lista)
	if len(lista) != 1 || lista[0].Fio == nil || lista[0].Fio.Total != 2 {
		t.Fatalf("resumo do fio na mensagem: %+v", lista)
	}
	// o fio não aparece como canal na visão
	var v visao
	bia.esperar(200, "GET", fmt.Sprintf("/api/servidores/%d", sid), nil, &v)
	for _, c := range v.Canais {
		if c.ID == f.ID {
			t.Fatal("fio não deveria aparecer na lista de canais")
		}
	}
	var fios []fio
	ana.esperar(200, "GET", fmt.Sprintf("/api/canais/%d/fios", geral), nil, &fios)
	if len(fios) != 1 || fios[0].Total != 2 {
		t.Fatalf("lista de fios: %+v", fios)
	}
	// renomear: quem criou pode; os outros só se gerenciarem mensagens (a dona gerencia)
	bia.esperar(200, "PATCH", fmt.Sprintf("/api/canais/%d/fio", f.ID), map[string]string{"nome": "Exercício 4"}, nil)
	var todos int64
	for _, c := range v.Cargos {
		if c.Padrao {
			todos = c.ID
		}
	}
	// canal pai fica privado: a Bia perde o acesso ao fio também
	ana.esperar(204, "PUT", fmt.Sprintf("/api/servidores/%d/canais/%d/permissoes", sid, geral),
		map[string]any{"tipo": "cargo", "alvoId": todos, "negar": uint64(permissoes.VerCanal)}, nil)
	bia.esperar(404, "GET", fioMsgs, nil, nil)
	bia.esperar(404, "POST", fioMsgs, map[string]string{"conteudo": "ainda dá?"}, nil)
	ana.esperar(200, "GET", fioMsgs, nil, nil)
	ana.esperar(204, "DELETE", fmt.Sprintf("/api/canais/%d/fio", f.ID), nil, nil)
	ana.esperar(404, "GET", fioMsgs, nil, nil)
}
