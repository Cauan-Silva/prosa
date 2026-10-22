package servidor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/banco"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
	"github.com/Cauan-Silva/prosa/internal/presenca"
)

// Os testes de ponta a ponta precisam de um PostgreSQL:
//
//	PROSA_TESTE_BANCO="postgres://postgres@localhost:5432/postgres?sslmode=disable" go test ./...
//
// Cada execução cria um banco novo e apaga no fim.
func ambiente(t *testing.T) (*httptest.Server, *eventos.Hub) {
	t.Helper()
	db := bancoDeTeste(t)
	hub := eventos.NovoHub()
	return instancia(t, db, hub, eventos.Local{Hub: hub}, nil, "teste"), hub
}

func bancoDeTeste(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("PROSA_TESTE_BANCO")
	if base == "" {
		t.Skip("defina PROSA_TESTE_BANCO para rodar os testes com banco")
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatal(err)
	}
	nome := fmt.Sprintf("prosa_teste_%d", os.Getpid())
	_, _ = admin.Exec("DROP DATABASE IF EXISTS " + nome + " WITH (FORCE)")
	if _, err := admin.Exec("CREATE DATABASE " + nome); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(base)
	u.Path = "/" + nome
	db, err := banco.Abrir(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := banco.Migrar(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + nome + " WITH (FORCE)")
		admin.Close()
	})
	return db
}

// instancia sobe uma cópia do Prosa (várias podem dividir o mesmo banco e o mesmo Redis).
func instancia(t *testing.T, db *sql.DB, hub *eventos.Hub, pub eventos.Publicador, pres presenca.Registro, nome string) *httptest.Server {
	t.Helper()
	pasta, err := arquivos.NovaPasta(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rotas, _ := Novo(Opcoes{
		DB: db, Emissor: auth.NovoEmissor([]byte("segredo-de-teste-com-mais-de-32-caracteres")),
		Publicador: pub, Presenca: pres, Hub: hub, Armazem: pasta, MaxAnexoMB: 1, Instancia: nome,
		Telas: http.NotFoundHandler(),
	})
	srv := httptest.NewServer(rotas)
	t.Cleanup(srv.Close)
	return srv
}

type cliente struct {
	t     *testing.T
	base  string
	token string
	id    int64
}

func (c *cliente) pedir(metodo, caminho string, corpo any, saida any) int {
	c.t.Helper()
	var leitor io.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		leitor = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(metodo, c.base+caminho, leitor)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if saida != nil && resp.StatusCode < 300 && len(b) > 0 {
		if err := json.Unmarshal(b, saida); err != nil {
			c.t.Fatalf("%s %s: resposta inválida %s", metodo, caminho, b)
		}
	}
	return resp.StatusCode
}

// esperar falha o teste se o status não for o esperado.
func (c *cliente) esperar(status int, metodo, caminho string, corpo any, saida any) {
	c.t.Helper()
	if got := c.pedir(metodo, caminho, corpo, saida); got != status {
		c.t.Fatalf("%s %s: status %d, esperava %d", metodo, caminho, got, status)
	}
}

func cadastrar(t *testing.T, base, nome string) *cliente {
	t.Helper()
	c := &cliente{t: t, base: base}
	var r struct {
		Token   string `json:"token"`
		Usuario struct {
			ID int64 `json:"id"`
		} `json:"usuario"`
	}
	c.esperar(201, "POST", "/api/cadastro", map[string]string{
		"nomeUsuario": nome, "nomeExibicao": strings.ToUpper(nome[:1]) + nome[1:], "email": nome + "@exemplo.com", "senha": "senha-forte-123",
	}, &r)
	c.token, c.id = r.Token, r.Usuario.ID
	return c
}

type visao struct {
	Servidor struct {
		ID int64 `json:"id"`
	} `json:"servidor"`
	Cargos []struct {
		ID     int64 `json:"id"`
		Padrao bool  `json:"padrao"`
	} `json:"cargos"`
	Canais []struct {
		ID   int64  `json:"id"`
		Nome string `json:"nome"`
		Tipo string `json:"tipo"`
	} `json:"canais"`
	Eu struct {
		Permissoes uint64 `json:"permissoes"`
	} `json:"eu"`
}

type mensagem struct {
	ID       int64   `json:"id"`
	Conteudo string  `json:"conteudo"`
	Mencoes  []int64 `json:"mencoes"`
	Reacoes  []struct {
		Emoji    string  `json:"emoji"`
		Usuarios []int64 `json:"usuarios"`
	} `json:"reacoes"`
	Anexos []struct {
		ID       int64  `json:"id"`
		Endereco string `json:"endereco"`
		Tipo     string `json:"tipo"`
	} `json:"anexos"`
	RespondeA *struct {
		ID int64 `json:"id"`
	} `json:"respondeA"`
}

// espiao guarda os eventos que chegam num tópico.
type espiao struct {
	mu     sync.Mutex
	vistos []eventos.Evento
}

func (e *espiao) Entregar(_ string, ev eventos.Evento) {
	e.mu.Lock()
	e.vistos = append(e.vistos, ev)
	e.mu.Unlock()
}

func (e *espiao) tipos() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var t []string
	for _, ev := range e.vistos {
		t = append(t, ev.Tipo)
	}
	return t
}

func TestFluxoCompleto(t *testing.T) {
	srv, hub := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")

	// cadastro repetido e login
	if s := (&cliente{t: t, base: srv.URL}).pedir("POST", "/api/cadastro", map[string]string{
		"nomeUsuario": "ANA", "nomeExibicao": "Outra", "email": "x@exemplo.com", "senha": "senha-forte-123"}, nil); s != 409 {
		t.Fatalf("nome repetido deveria dar 409, deu %d", s)
	}
	(&cliente{t: t, base: srv.URL}).esperar(200, "POST", "/api/entrar", map[string]string{"login": "ana@exemplo.com", "senha": "senha-forte-123"}, nil)
	(&cliente{t: t, base: srv.URL}).esperar(401, "POST", "/api/entrar", map[string]string{"login": "ana", "senha": "errada"}, nil)
	(&cliente{t: t, base: srv.URL}).esperar(401, "GET", "/api/servidores", nil, nil)

	// Ana cria o servidor
	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Turma do Café"}, &v)
	sid := v.Servidor.ID
	sv := fmt.Sprintf("/api/servidores/%d", sid)
	if v.Eu.Permissoes != uint64(permissoes.Todas) {
		t.Errorf("a dona deveria ter todas as permissões")
	}
	var geral, voz int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		} else {
			voz = c.ID
		}
	}
	if geral == 0 || voz == 0 {
		t.Fatalf("servidor novo sem canais: %+v", v.Canais)
	}
	todos := v.Cargos[0].ID
	esp := &espiao{}
	hub.Assinar(eventos.TopicoServidor(sid), esp)

	// Bia não é membro: tudo dá 404
	bia.esperar(404, "GET", sv, nil, nil)
	bia.esperar(404, "GET", fmt.Sprintf("/api/canais/%d/mensagens", geral), nil, nil)

	// convite
	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("%s/canais/%d/convites", sv, geral), map[string]int{"horas": 24, "maxUsos": 5}, &conv)
	(&cliente{t: t, base: srv.URL}).esperar(200, "GET", "/api/convites/"+conv.Codigo, nil, nil)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	var vb visao
	bia.esperar(200, "GET", sv, nil, &vb)
	if vb.Eu.Permissoes&uint64(permissoes.GerenciarCanais) != 0 {
		t.Error("membro comum não gerencia canais")
	}
	bia.esperar(403, "POST", sv+"/canais", map[string]string{"nome": "bagunça", "tipo": "texto"}, nil)

	// mensagens, menção e resposta
	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)
	var m1 mensagem
	ana.esperar(201, "POST", msgs, map[string]any{"conteudo": fmt.Sprintf("Oi <@%d>, bora pedir pizza hoje?", bia.id)}, &m1)
	if len(m1.Mencoes) != 1 || m1.Mencoes[0] != bia.id {
		t.Errorf("menção não registrada: %+v", m1.Mencoes)
	}
	var m2 mensagem
	bia.esperar(201, "POST", msgs, map[string]any{"conteudo": "Bora! @todos", "respondeA": m1.ID}, &m2)
	if m2.RespondeA == nil || m2.RespondeA.ID != m1.ID {
		t.Error("resposta não ligada à mensagem original")
	}
	var minhas []mensagem
	bia.esperar(200, "GET", "/api/eu/mencoes", nil, &minhas)
	if len(minhas) != 1 || minhas[0].ID != m1.ID {
		t.Errorf("menções da Bia: %+v", minhas)
	}
	bia.esperar(400, "POST", msgs, map[string]any{"conteudo": "   "}, nil)
	bia.esperar(400, "POST", msgs, map[string]any{"conteudo": strings.Repeat("a", 4001)}, nil)

	// editar e apagar
	bia.esperar(403, "PATCH", fmt.Sprintf("%s/%d", msgs, m1.ID), map[string]string{"conteudo": "hackeado"}, nil)
	bia.esperar(403, "DELETE", fmt.Sprintf("%s/%d", msgs, m1.ID), nil, nil)
	var ed mensagem
	bia.esperar(200, "PATCH", fmt.Sprintf("%s/%d", msgs, m2.ID), map[string]string{"conteudo": "Bora!! 🍕"}, &ed)
	if ed.Conteudo != "Bora!! 🍕" {
		t.Errorf("edição: %q", ed.Conteudo)
	}

	// reações
	reacao := fmt.Sprintf("%s/%d/reacoes/%s", msgs, m1.ID, url.PathEscape("🍕"))
	bia.esperar(204, "PUT", reacao, nil, nil)
	ana.esperar(204, "PUT", reacao, nil, nil)
	ana.esperar(204, "PUT", reacao, nil, nil) // repetir não duplica
	var lista []mensagem
	bia.esperar(200, "GET", msgs, nil, &lista)
	if len(lista) != 2 || len(lista[0].Reacoes) != 1 || len(lista[0].Reacoes[0].Usuarios) != 2 {
		t.Fatalf("lista/reações: %+v", lista)
	}
	ana.esperar(204, "DELETE", reacao, nil, nil)

	// paginação
	for i := 0; i < 3; i++ {
		ana.esperar(201, "POST", msgs, map[string]string{"conteudo": fmt.Sprintf("mensagem %d", i)}, nil)
	}
	var depois []mensagem
	bia.esperar(200, "GET", fmt.Sprintf("%s?depois=%d", msgs, m2.ID), nil, &depois)
	if len(depois) != 3 || depois[0].Conteudo != "mensagem 0" {
		t.Errorf("depois=: %+v", depois)
	}
	var antes []mensagem
	bia.esperar(200, "GET", fmt.Sprintf("%s?antes=%d", msgs, m2.ID), nil, &antes)
	if len(antes) != 1 || antes[0].ID != m1.ID {
		t.Errorf("antes=: %+v", antes)
	}

	// busca (em português: "pizzas" acha "pizza")
	var achou []mensagem
	bia.esperar(200, "GET", sv+"/busca?q=pizzas", nil, &achou)
	if len(achou) != 1 || achou[0].ID != m1.ID {
		t.Errorf("busca: %+v", achou)
	}

	// canal privado: @todos sem VerCanal; cargo Equipe com VerCanal
	var staff struct {
		ID int64 `json:"id"`
	}
	ana.esperar(201, "POST", sv+"/canais", map[string]string{"nome": "Equipe Só", "tipo": "texto"}, &staff)
	perm := fmt.Sprintf("%s/canais/%d/permissoes", sv, staff.ID)
	ana.esperar(204, "PUT", perm, map[string]any{"tipo": "cargo", "alvoId": todos, "negar": uint64(permissoes.VerCanal)}, nil)
	ana.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", staff.ID), map[string]string{"conteudo": "segredo da pizza"}, nil)
	bia.esperar(404, "GET", fmt.Sprintf("/api/canais/%d/mensagens", staff.ID), nil, nil)
	bia.esperar(200, "GET", sv+"/busca?q=segredo", nil, &achou)
	if len(achou) != 0 {
		t.Error("a busca mostrou mensagem de canal que a Bia não vê")
	}
	bia.esperar(200, "GET", sv, nil, &vb)
	for _, c := range vb.Canais {
		if c.ID == staff.ID {
			t.Error("canal privado apareceu para a Bia")
		}
	}
	var cargo struct {
		ID int64 `json:"id"`
	}
	ana.esperar(201, "POST", sv+"/cargos", map[string]any{"nome": "Equipe", "cor": 0x2f9e44, "permissoes": uint64(permissoes.Padrao)}, &cargo)
	ana.esperar(204, "PUT", perm, map[string]any{"tipo": "cargo", "alvoId": cargo.ID, "permitir": uint64(permissoes.VerCanal)}, nil)
	ana.esperar(200, "PUT", fmt.Sprintf("%s/membros/%d/cargos/%d", sv, bia.id, cargo.ID), nil, nil)
	bia.esperar(200, "GET", fmt.Sprintf("/api/canais/%d/mensagens", staff.ID), nil, &lista)
	if len(lista) != 1 {
		t.Errorf("com o cargo, a Bia deveria ler o canal: %+v", lista)
	}
	// Bia não pode mexer em cargos nem dar cargo a si mesma
	bia.esperar(403, "PATCH", fmt.Sprintf("%s/cargos/%d", sv, cargo.ID), map[string]any{"permissoes": uint64(permissoes.Todas)}, nil)

	// anexos
	corpo := &bytes.Buffer{}
	mp := multipart.NewWriter(corpo)
	parte, _ := mp.CreateFormFile("arquivo", "../../notas.txt")
	parte.Write([]byte("lista de compras: pizza"))
	mp.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/canais/%d/anexos", srv.URL, geral), corpo)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+bia.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var anexo struct {
		ID       int64  `json:"id"`
		Nome     string `json:"nome"`
		Endereco string `json:"endereco"`
	}
	json.NewDecoder(resp.Body).Decode(&anexo)
	resp.Body.Close()
	if resp.StatusCode != 201 || anexo.Nome != "notas.txt" {
		t.Fatalf("upload: %d %+v", resp.StatusCode, anexo)
	}
	// antes de ir para uma mensagem, só quem enviou baixa
	if s := baixar(t, srv.URL+anexo.Endereco, ana.token); s != 404 {
		t.Errorf("Ana baixou anexo solto da Bia: %d", s)
	}
	var comAnexo mensagem
	bia.esperar(201, "POST", msgs, map[string]any{"conteudo": "", "anexos": []int64{anexo.ID}}, &comAnexo)
	if len(comAnexo.Anexos) != 1 || comAnexo.Anexos[0].Tipo != "text/plain" {
		t.Errorf("mensagem com anexo: %+v", comAnexo)
	}
	bia.esperar(400, "POST", msgs, map[string]any{"conteudo": "de novo", "anexos": []int64{anexo.ID}}, nil)
	if s := baixar(t, srv.URL+anexo.Endereco, ana.token); s != 200 {
		t.Errorf("Ana deveria baixar o anexo da mensagem: %d", s)
	}
	if s := baixar(t, srv.URL+anexo.Endereco, "token-falso"); s != 401 {
		t.Errorf("token falso baixou anexo: %d", s)
	}

	// castigo: Bia só lê
	ana.esperar(200, "PUT", fmt.Sprintf("%s/membros/%d/castigo", sv, bia.id), map[string]any{"minutos": 10, "motivo": "spam"}, nil)
	bia.esperar(403, "POST", msgs, map[string]string{"conteudo": "posso falar?"}, nil)
	bia.esperar(200, "GET", msgs, nil, nil)
	ana.esperar(200, "PUT", fmt.Sprintf("%s/membros/%d/castigo", sv, bia.id), map[string]any{"minutos": 0}, nil)
	bia.esperar(201, "POST", msgs, map[string]string{"conteudo": "voltei"}, nil)

	// Bia não pode expulsar a dona
	bia.esperar(403, "DELETE", fmt.Sprintf("%s/membros/%d", sv, ana.id), nil, nil)

	// banimento: perde o acesso e não volta pelo convite
	ana.esperar(204, "PUT", fmt.Sprintf("%s/banimentos/%d", sv, bia.id), map[string]string{"motivo": "teste"}, nil)
	bia.esperar(404, "GET", sv, nil, nil)
	bia.esperar(403, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	ana.esperar(204, "DELETE", fmt.Sprintf("%s/banimentos/%d", sv, bia.id), nil, nil)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)

	// auditoria registrou a moderação
	var aud []struct {
		Acao string `json:"acao"`
	}
	ana.esperar(200, "GET", sv+"/auditoria", nil, &aud)
	acoes := map[string]bool{}
	for _, a := range aud {
		acoes[a.Acao] = true
	}
	for _, a := range []string{"membro_castigado", "membro_banido"} {
		if !acoes[a] {
			t.Errorf("auditoria sem %q: %+v", a, aud)
		}
	}

	// eventos publicados
	vistos := strings.Join(esp.tipos(), ",")
	for _, tipo := range []string{eventos.MensagemCriada, eventos.MensagemEditada, eventos.ReacaoAdicionada, eventos.ReacaoRemovida,
		eventos.MembroEntrou, eventos.CanalCriado, eventos.CargoCriado, eventos.MembroSaiu} {
		if !strings.Contains(vistos, tipo) {
			t.Errorf("evento %s não publicado (vistos: %s)", tipo, vistos)
		}
	}
	for _, ev := range esp.vistos {
		if ev.Tipo == eventos.MensagemCriada && (ev.CanalID == 0 || ev.Exige != uint64(permissoes.VerCanal)) {
			t.Errorf("evento de mensagem sem canal/permissão: %+v", ev)
		}
	}
}

func baixar(t *testing.T, endereco, token string) int {
	t.Helper()
	resp, err := http.Get(endereco + "?t=" + url.QueryEscape(token))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestModoLentoELimite(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Lento"}, &v)
	sv := fmt.Sprintf("/api/servidores/%d", v.Servidor.ID)
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}
	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("%s/canais/%d/convites", sv, geral), map[string]int{}, &conv)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	ana.esperar(200, "PATCH", fmt.Sprintf("%s/canais/%d", sv, geral), map[string]int{"modoLento": 30}, nil)

	msgs := fmt.Sprintf("/api/canais/%d/mensagens", geral)
	bia.esperar(201, "POST", msgs, map[string]string{"conteudo": "um"}, nil)
	bia.esperar(429, "POST", msgs, map[string]string{"conteudo": "dois"}, nil)
	// quem gerencia o canal não sofre modo lento, mas ainda tem o limite geral
	n429 := 0
	for i := 0; i < 12; i++ {
		if ana.pedir("POST", msgs, map[string]string{"conteudo": "rápido"}, nil) == 429 {
			n429++
		}
	}
	if n429 == 0 {
		t.Error("o limite de mensagens por pessoa não segurou a rajada")
	}
}
