// Package presenca sabe quem está online (com pelo menos uma conexão aberta).
//
// Com uma instância só, a contagem fica na memória (Local). Com várias, fica no Redis
// (veja redis.go), e cada instância renova de tempos em tempos as pessoas que tem
// conectadas; se uma instância cair, as pessoas dela saem sozinhas quando a validade passa.
package presenca

import (
	"context"
	"sync"
)

type Registro interface {
	// Conectou conta mais uma conexão; primeira = a pessoa acabou de ficar online.
	Conectou(ctx context.Context, usuarioID int64) (primeira bool, err error)
	// Desconectou tira uma conexão; ultima = a pessoa ficou offline.
	Desconectou(ctx context.Context, usuarioID int64) (ultima bool, err error)
	// Online diz quem da lista está online.
	Online(ctx context.Context, ids []int64) (map[int64]bool, error)
}

// Local guarda a contagem na memória desta instância.
type Local struct {
	mu       sync.Mutex
	conexoes map[int64]int
}

func NovoLocal() *Local { return &Local{conexoes: map[int64]int{}} }

func (l *Local) Conectou(_ context.Context, id int64) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conexoes[id]++
	return l.conexoes[id] == 1, nil
}

func (l *Local) Desconectou(_ context.Context, id int64) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conexoes[id] <= 1 {
		delete(l.conexoes, id)
		return true, nil
	}
	l.conexoes[id]--
	return false, nil
}

func (l *Local) Online(_ context.Context, ids []int64) (map[int64]bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	saida := make(map[int64]bool)
	for _, id := range ids {
		if l.conexoes[id] > 0 {
			saida[id] = true
		}
	}
	return saida, nil
}
