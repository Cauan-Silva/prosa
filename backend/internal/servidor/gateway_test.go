package servidor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

type evento struct {
	Tipo       string          `json:"t"`
	ServidorID int64           `json:"s"`
	CanalID    int64           `json:"c"`
	UsuarioID  int64           `json:"u"`
	Dados      json.RawMessage `json:"d"`
}

type tempoReal struct {
	t      *testing.T
	ws     *websocket.Conn
	chegou chan evento
}

func conectar(t *testing.T, base, token string) *tempoReal {
	t.Helper()
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/api/gateway?t="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &tempoReal{t: t, ws: ws, chegou: make(chan evento, 200)}
	go func() {
		for {
			_, b, err := ws.Read(context.Background())
			if err != nil {
				close(tr.chegou)
				return
			}
			var e evento
			json.Unmarshal(b, &e)
			tr.chegou <- e
		}
	}()
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	return tr
}

// esperar lê até chegar um evento do tipo (descarta os outros).
func (tr *tempoReal) esperar(tipo string, filtro func(evento) bool) evento {
	tr.t.Helper()
	limite := time.After(3 * time.Second)
	for {
		select {
		case e, ok := <-tr.chegou:
			if !ok {
				tr.t.Fatalf("conexão fechou esperando %s", tipo)
			}
			if e.Tipo == tipo && (filtro == nil || filtro(e)) {
				return e
			}
		case <-limite:
			tr.t.Fatalf("não chegou %s", tipo)
		}
	}
}

// nada confirma que nenhum evento do tipo chega em pouco tempo.
func (tr *tempoReal) nada(tipo string, filtro func(evento) bool) {
	tr.t.Helper()
	limite := time.After(400 * time.Millisecond)
	for {
		select {
		case e := <-tr.chegou:
			if e.Tipo == tipo && (filtro == nil || filtro(e)) {
				tr.t.Fatalf("não deveria chegar %s: %s", tipo, e.Dados)
			}
		case <-limite:
			return
		}
	}
}

func (tr *tempoReal) mandar(tipo string, dados any) {
	b, _ := json.Marshal(map[string]any{"t": tipo, "d": dados})
	if err := tr.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		tr.t.Fatal(err)
	}
}

func TestGateway(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")

	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Tempo Real"}, &v)
	sid := v.Servidor.ID
	sv := fmt.Sprintf("/api/servidores/%d", sid)
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}

	// sem token não conecta
	if _, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/gateway?t=x", nil); err == nil {
		t.Fatal("conectou com token inválido")
	}

	wa := conectar(t, srv.URL, ana.token)
	p := wa.esperar("PRONTO", nil)
	var pronto struct {
		Usuario struct {
			ID int64 `json:"id"`
		} `json:"usuario"`
		Servidores []visao            `json:"servidores"`
		Online     map[string][]int64 `json:"online"`
	}
	json.Unmarshal(p.Dados, &pronto)
	if pronto.Usuario.ID != ana.id || len(pronto.Servidores) != 1 || len(pronto.Online[fmt.Sprint(sid)]) != 1 {
		t.Fatalf("PRONTO errado: %s", p.Dados)
	}

	wb := conectar(t, srv.URL, bia.token)
	wb.esperar("PRONTO", nil)

	// Bia entra pelo convite: a conexão dela recebe o servidor, a da Ana vê o membro entrar
	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("%s/canais/%d/convites", sv, geral), map[string]int{}, &conv)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	criado := wb.esperar("SERVIDOR_CRIADO", nil)
	if criado.ServidorID != sid || !strings.Contains(string(criado.Dados), `"visao"`) {
		t.Fatalf("SERVIDOR_CRIADO: %+v", criado)
	}
	wa.esperar("MEMBRO_ENTROU", nil)

	// mensagem no geral chega para os dois
	ana.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", geral), map[string]string{"conteudo": "olá", "nonce": "n1"}, nil)
	m := wb.esperar("MENSAGEM_CRIADA", nil)
	if m.CanalID != geral || !strings.Contains(string(m.Dados), `"olá"`) {
		t.Fatalf("mensagem: %+v", m)
	}
	wa.esperar("MENSAGEM_CRIADA", func(e evento) bool { return strings.Contains(string(e.Dados), `"nonce":"n1"`) })

	// digitando
	wb.mandar("DIGITANDO", map[string]int64{"canalId": geral})
	d := wa.esperar("DIGITANDO", nil)
	if d.CanalID != geral || !strings.Contains(string(d.Dados), fmt.Sprint(bia.id)) {
		t.Fatalf("digitando: %+v", d)
	}

	// canal privado: criar já sem @todos; a Bia nem fica sabendo que existe
	var todos int64
	for _, c := range v.Cargos {
		if c.Padrao {
			todos = c.ID
		}
	}
	var staff struct {
		ID int64 `json:"id"`
	}
	ana.esperar(201, "POST", sv+"/canais", map[string]string{"nome": "staff", "tipo": "texto"}, &staff)
	wb.esperar("CANAL_CRIADO", func(e evento) bool { return e.CanalID == staff.ID })
	ana.esperar(204, "PUT", fmt.Sprintf("%s/canais/%d/permissoes", sv, staff.ID),
		map[string]any{"tipo": "cargo", "alvoId": todos, "negar": uint64(permissoes.VerCanal)}, nil)
	wb.esperar("PERMISSOES_ALTERADAS", nil) // o canal sumiu para ela: recarrega a visão
	ana.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", staff.ID), map[string]string{"conteudo": "só a equipe"}, nil)
	wa.esperar("MENSAGEM_CRIADA", func(e evento) bool { return e.CanalID == staff.ID })
	wb.nada("MENSAGEM_CRIADA", func(e evento) bool { return e.CanalID == staff.ID })
	ana.esperar(200, "PATCH", fmt.Sprintf("%s/canais/%d", sv, staff.ID), map[string]string{"topico": "assunto secreto"}, nil)
	wb.nada("CANAL_ALTERADO", func(e evento) bool { return e.CanalID == staff.ID })

	// presença: a Bia sai e a Ana fica sabendo
	wb.ws.Close(websocket.StatusNormalClosure, "")
	off := wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":false`) })
	if !strings.Contains(string(off.Dados), fmt.Sprint(bia.id)) {
		t.Fatalf("presença: %s", off.Dados)
	}
	wb = conectar(t, srv.URL, bia.token)
	wb.esperar("PRONTO", nil)
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":true`) })

	// expulsa: a conexão da Bia perde o servidor e para de receber dele
	ana.esperar(204, "DELETE", fmt.Sprintf("%s/membros/%d", sv, bia.id), nil, nil)
	wb.esperar("SERVIDOR_APAGADO", func(e evento) bool { return e.ServidorID == sid })
	wa.esperar("MEMBRO_SAIU", func(e evento) bool { return e.UsuarioID == bia.id })
	ana.esperar(201, "POST", fmt.Sprintf("/api/canais/%d/mensagens", geral), map[string]string{"conteudo": "depois da saída"}, nil)
	wb.nada("MENSAGEM_CRIADA", nil)
}
