package servidor

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Cauan-Silva/prosa/internal/api"
)

// limitarPorIP segura tentativas de login e cadastro em massa: no máximo porMinuto
// por endereço IP (um balde que enche de novo aos poucos).
func limitarPorIP(proximo http.Handler, porMinuto int) http.Handler {
	if porMinuto <= 0 {
		porMinuto = 10
	}
	l := &limitador{baldes: map[string]*balde{}, capacidade: float64(porMinuto), recarga: time.Minute / time.Duration(porMinuto), agora: time.Now}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.permitir(ipDe(r)) {
			api.Responder(w, http.StatusTooManyRequests, map[string]string{"erro": "Muitas tentativas. Espere um minuto e tente de novo."})
			return
		}
		proximo.ServeHTTP(w, r)
	})
}

type balde struct {
	fichas float64
	visto  time.Time
}

type limitador struct {
	mu         sync.Mutex
	baldes     map[string]*balde
	capacidade float64
	recarga    time.Duration // tempo para ganhar uma ficha
	agora      func() time.Time
}

func (l *limitador) permitir(chave string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	agora := l.agora()
	b := l.baldes[chave]
	if b == nil {
		if len(l.baldes) > 10_000 { // não deixa o mapa crescer sem fim
			l.baldes = map[string]*balde{}
		}
		b = &balde{fichas: l.capacidade, visto: agora}
		l.baldes[chave] = b
	}
	b.fichas = min(l.capacidade, b.fichas+agora.Sub(b.visto).Seconds()/l.recarga.Seconds())
	b.visto = agora
	if b.fichas < 1 {
		return false
	}
	b.fichas--
	return true
}

// ipDe usa o último endereço do X-Forwarded-For, que é o que o proxy na frente do servidor
// (Render, Caddy) viu de verdade. Os anteriores vêm do próprio navegador e podem ser inventados.
func ipDe(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		partes := strings.Split(xff, ",")
		return strings.TrimSpace(partes[len(partes)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
