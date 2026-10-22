package voz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TurnCloudflare busca credenciais temporárias no TURN do Cloudflare Realtime
// (1.000 GB grátis por mês). As credenciais valem 24 h e são renovadas na metade do prazo.
type TurnCloudflare struct {
	ChaveID string // CF_TURN_ID
	Token   string // CF_TURN_TOKEN
	Cliente *http.Client
	API     string // padrão: https://rtc.live.cloudflare.com (os testes trocam)

	mu      sync.Mutex
	ices    []ICE
	validoA time.Time
}

const validadeCredencial = 24 * time.Hour

func (t *TurnCloudflare) Servidores(ctx context.Context) ([]ICE, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ices != nil && time.Now().Before(t.validoA) {
		return t.ices, nil
	}
	cliente := t.Cliente
	if cliente == nil {
		cliente = &http.Client{Timeout: 10 * time.Second}
	}
	api := t.API
	if api == "" {
		api = "https://rtc.live.cloudflare.com"
	}
	corpo, _ := json.Marshal(map[string]int{"ttl": int(validadeCredencial.Seconds())})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		api+"/v1/turn/keys/"+t.ChaveID+"/credentials/generate-ice-servers", bytes.NewReader(corpo))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cliente.Do(req)
	if err != nil {
		return t.ices, err // devolve as anteriores, se houver
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return t.ices, fmt.Errorf("cloudflare respondeu %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		IceServers []ICE `json:"iceServers"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.IceServers) == 0 {
		return t.ices, fmt.Errorf("resposta inesperada do cloudflare")
	}
	// Endereços na porta 53 costumam ser bloqueados pelos navegadores e redes: ficam de fora.
	for i := range r.IceServers {
		urls := r.IceServers[i].URLs[:0]
		for _, u := range r.IceServers[i].URLs {
			if !strings.Contains(u, ":53?") && !strings.HasSuffix(u, ":53") {
				urls = append(urls, u)
			}
		}
		r.IceServers[i].URLs = urls
	}
	t.ices = r.IceServers
	t.validoA = time.Now().Add(validadeCredencial / 2)
	slog.Info("voz: credenciais do TURN do Cloudflare renovadas")
	return t.ices, nil
}
