package presenca

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis conta as conexões de cada pessoa em todas as instâncias.
//
//	prosa:pres:<usuario>   hash  instância -> número de conexões nela
//	prosa:inst:<instância> texto com validade de 30 s, renovado a cada 10 s
//
// Uma pessoa está online se alguma instância *viva* tem conexão dela. Se uma instância
// cair sem avisar, a chave dela expira e as pessoas que estavam nela deixam de contar.
type Redis struct {
	cliente   *redis.Client
	instancia string
}

const (
	validadeInstancia = 30 * time.Second
	renovarInstancia  = 10 * time.Second
)

func chavePresenca(id int64) string  { return "prosa:pres:" + strconv.FormatInt(id, 10) }
func chaveInstancia(i string) string { return "prosa:inst:" + i }

// NovoRedis registra a instância e mantém o registro vivo enquanto o contexto durar.
func NovoRedis(ctx context.Context, cliente *redis.Client, instancia string) *Redis {
	r := &Redis{cliente: cliente, instancia: instancia}
	r.renovar(ctx)
	go func() {
		t := time.NewTicker(renovarInstancia)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				// desligamento limpo: sai na hora, sem esperar a validade
				fim, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
				cliente.Del(fim, chaveInstancia(instancia))
				cancelar()
				return
			case <-t.C:
				r.renovar(ctx)
			}
		}
	}()
	return r
}

func (r *Redis) renovar(ctx context.Context) {
	if err := r.cliente.Set(ctx, chaveInstancia(r.instancia), "1", validadeInstancia).Err(); err != nil {
		slog.Warn("redis: renovar instância", "erro", err)
	}
}

// vivas diz quais instâncias da lista ainda estão no ar.
func (r *Redis) vivas(ctx context.Context, instancias []string) (map[string]bool, error) {
	saida := map[string]bool{}
	if len(instancias) == 0 {
		return saida, nil
	}
	chaves := make([]string, len(instancias))
	for i, inst := range instancias {
		chaves[i] = chaveInstancia(inst)
	}
	valores, err := r.cliente.MGet(ctx, chaves...).Result()
	if err != nil {
		return nil, err
	}
	for i, v := range valores {
		if v != nil {
			saida[instancias[i]] = true
		}
	}
	return saida, nil
}

// total soma as conexões da pessoa nas instâncias vivas.
func (r *Redis) total(ctx context.Context, id int64) (int64, error) {
	contagem, err := r.cliente.HGetAll(ctx, chavePresenca(id)).Result()
	if err != nil {
		return 0, err
	}
	insts := make([]string, 0, len(contagem))
	for inst := range contagem {
		insts = append(insts, inst)
	}
	vivas, err := r.vivas(ctx, insts)
	if err != nil {
		return 0, err
	}
	var soma int64
	var mortas []string
	for inst, n := range contagem {
		if !vivas[inst] {
			mortas = append(mortas, inst)
			continue
		}
		v, _ := strconv.ParseInt(n, 10, 64)
		soma += v
	}
	if len(mortas) > 0 {
		r.cliente.HDel(ctx, chavePresenca(id), mortas...) // faxina
	}
	return soma, nil
}

func (r *Redis) Conectou(ctx context.Context, id int64) (bool, error) {
	if err := r.cliente.HIncrBy(ctx, chavePresenca(id), r.instancia, 1).Err(); err != nil {
		return false, err
	}
	n, err := r.total(ctx, id)
	return n == 1, err
}

func (r *Redis) Desconectou(ctx context.Context, id int64) (bool, error) {
	n, err := r.cliente.HIncrBy(ctx, chavePresenca(id), r.instancia, -1).Result()
	if err != nil {
		return false, err
	}
	if n <= 0 {
		r.cliente.HDel(ctx, chavePresenca(id), r.instancia)
	}
	total, err := r.total(ctx, id)
	return total == 0, err
}

func (r *Redis) Online(ctx context.Context, ids []int64) (map[int64]bool, error) {
	saida := map[int64]bool{}
	if len(ids) == 0 {
		return saida, nil
	}
	pipe := r.cliente.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, chavePresenca(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	todas := map[string]bool{}
	for _, c := range cmds {
		for inst := range c.Val() {
			todas[inst] = true
		}
	}
	lista := make([]string, 0, len(todas))
	for inst := range todas {
		lista = append(lista, inst)
	}
	vivas, err := r.vivas(ctx, lista)
	if err != nil {
		return nil, err
	}
	for i, c := range cmds {
		for inst, n := range c.Val() {
			if v, _ := strconv.Atoi(n); v > 0 && vivas[inst] {
				saida[ids[i]] = true
				break
			}
		}
	}
	return saida, nil
}
