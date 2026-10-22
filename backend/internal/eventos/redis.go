package eventos

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const prefixoCanal = "prosa:ev:"

// Redis publica os eventos no Redis Pub/Sub para que todas as instâncias recebam.
//
// Cada instância assina no Redis só os tópicos que alguém conectado nela precisa
// (o Hub avisa quando um tópico ganha o primeiro assinante ou perde o último), então
// uma instância não recebe o tráfego de servidores em que ninguém dela está.
type Redis struct {
	cliente *redis.Client
	hub     *Hub
	pubsub  *redis.PubSub

	pedidos chan pedidoAssinatura
}

type pedidoAssinatura struct {
	topico string
	entrar bool
}

// NovoRedis liga o Hub desta instância ao Redis e começa a receber os eventos.
func NovoRedis(ctx context.Context, cliente *redis.Client, hub *Hub) *Redis {
	r := &Redis{cliente: cliente, hub: hub, pedidos: make(chan pedidoAssinatura, 4096)}
	r.pubsub = cliente.Subscribe(ctx) // sem canais ainda; vão sendo assinados conforme o Hub pede
	hub.mu.Lock()
	hub.aoMudar = func(topico string, assinado bool) {
		select {
		case r.pedidos <- pedidoAssinatura{topico, assinado}:
		default:
			slog.Error("fila de assinaturas do Redis cheia", "topico", topico)
		}
	}
	hub.aguardar = func(ctx context.Context, topicos []string) error {
		canais := make([]string, len(topicos))
		for i, t := range topicos {
			canais[i] = prefixoCanal + t
		}
		return r.pubsub.Subscribe(ctx, canais...)
	}
	hub.mu.Unlock()
	go r.assinaturas(ctx)
	go r.receber(ctx)
	return r
}

// Publicar manda o evento para o Redis; todas as instâncias com o tópico assinado entregam.
func (r *Redis) Publicar(ctx context.Context, topico string, e Evento) error {
	dados, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return r.cliente.Publish(ctx, prefixoCanal+topico, dados).Err()
}

// assinaturas aplica no Redis as mudanças pedidas pelo Hub, juntando várias de uma vez.
func (r *Redis) assinaturas(ctx context.Context) {
	for {
		var entrar, sair []string
		select {
		case <-ctx.Done():
			return
		case p := <-r.pedidos:
			entrar, sair = juntar(p, entrar, sair)
		}
		// pega o que mais estiver na fila (ex.: uma conexão assinando 50 servidores)
	mais:
		for len(entrar)+len(sair) < 500 {
			select {
			case p := <-r.pedidos:
				entrar, sair = juntar(p, entrar, sair)
			default:
				break mais
			}
		}
		// O Hub é a verdade: confere o estado atual de cada tópico antes de mexer.
		var assinar, cancelar []string
		for _, t := range entrar {
			if r.hub.Assinantes(t) > 0 {
				assinar = append(assinar, prefixoCanal+t)
			}
		}
		for _, t := range sair {
			if r.hub.Assinantes(t) == 0 {
				cancelar = append(cancelar, prefixoCanal+t)
			}
		}
		if len(assinar) > 0 {
			if err := r.pubsub.Subscribe(ctx, assinar...); err != nil {
				slog.Error("redis: assinar", "erro", err)
			}
		}
		if len(cancelar) > 0 {
			if err := r.pubsub.Unsubscribe(ctx, cancelar...); err != nil {
				slog.Error("redis: cancelar assinatura", "erro", err)
			}
		}
	}
}

func juntar(p pedidoAssinatura, entrar, sair []string) ([]string, []string) {
	if p.entrar {
		return append(entrar, p.topico), sair
	}
	return entrar, append(sair, p.topico)
}

// receber entrega no Hub local o que chega do Redis. Se a conexão com o Redis cair, o
// go-redis reconecta e assina de novo os mesmos canais sozinho.
func (r *Redis) receber(ctx context.Context) {
	canal := r.pubsub.Channel(redis.WithChannelSize(4096), redis.WithChannelHealthCheckInterval(15*time.Second))
	for {
		select {
		case <-ctx.Done():
			r.pubsub.Close()
			return
		case m, ok := <-canal:
			if !ok {
				return
			}
			var e Evento
			if err := json.Unmarshal([]byte(m.Payload), &e); err != nil {
				continue
			}
			r.hub.Entregar(strings.TrimPrefix(m.Channel, prefixoCanal), e)
		}
	}
}
