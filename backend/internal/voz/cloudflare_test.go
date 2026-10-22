package voz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTurnCloudflare(t *testing.T) {
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		if r.URL.Path != "/v1/turn/keys/chave/credentials/generate-ice-servers" || r.Header.Get("Authorization") != "Bearer segredo" {
			http.Error(w, "não", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"iceServers":[{"urls":["stun:stun.cloudflare.com:3478","stun:stun.cloudflare.com:53"]},
			{"urls":["turn:turn.cloudflare.com:3478?transport=udp","turn:turn.cloudflare.com:53?transport=udp","turns:turn.cloudflare.com:443?transport=tcp"],
			 "username":"u","credential":"c"}]}`))
	}))
	defer srv.Close()
	cf := &TurnCloudflare{ChaveID: "chave", Token: "segredo", API: srv.URL}
	ices, err := cf.Servidores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ices) != 2 || len(ices[1].URLs) != 2 || ices[1].Username != "u" || len(ices[0].URLs) != 1 {
		t.Fatalf("servidores: %+v", ices)
	}
	_, _ = cf.Servidores(context.Background())
	if pedidos.Load() != 1 {
		t.Errorf("deveria reaproveitar as credenciais, fez %d pedidos", pedidos.Load())
	}
	errado := &TurnCloudflare{ChaveID: "chave", Token: "errado", API: srv.URL}
	if _, err := errado.Servidores(context.Background()); err == nil {
		t.Error("token errado deveria dar erro")
	}
}
