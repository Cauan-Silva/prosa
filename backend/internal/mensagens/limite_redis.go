package mensagens

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// LimiteRedis conta as mensagens de cada pessoa no Redis, numa janela fixa, para o
// limite valer mesmo com a pessoa conectada em instâncias diferentes.
type LimiteRedis struct {
	Cliente *redis.Client
	Janela  time.Duration
	Maximo  int64
}

func (l LimiteRedis) Permitir(ctx context.Context, usuarioID int64) bool {
	janela := time.Now().UnixNano() / int64(l.Janela)
	chave := "prosa:lim:" + strconv.FormatInt(usuarioID, 10) + ":" + strconv.FormatInt(janela, 10)
	pipe := l.Cliente.TxPipeline()
	n := pipe.Incr(ctx, chave)
	pipe.Expire(ctx, chave, l.Janela+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("redis: limite de mensagens", "erro", err)
		return true // sem Redis, não bloqueia ninguém
	}
	return n.Val() <= l.Maximo
}
