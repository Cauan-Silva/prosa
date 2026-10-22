package voz

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Coordenador decide em que instância fica cada sala de voz e guarda quem está em cada
// sala (para quem conecta depois saber).
type Coordenador interface {
	// Dono diz o endereço da instância com a sala ("" = ninguém ainda).
	Dono(ctx context.Context, canalID int64) (string, error)
	// Reservar tenta ficar com a sala; se outra instância já tem, devolve o endereço dela.
	Reservar(ctx context.Context, canalID int64, endereco string) (nossa bool, dono string, err error)
	Renovar(ctx context.Context, canalID int64, endereco string)
	Liberar(ctx context.Context, canalID int64, endereco string)
	GravarEstado(ctx context.Context, servidorID, canalID int64, estados []Estado) error
	// LerEstado: servidor -> canal -> pessoas (as chaves em texto, para virar JSON).
	LerEstado(ctx context.Context, servidores []int64) (map[string]map[string][]Estado, error)
}

// Local: uma instância só, tudo na memória.
type Local struct {
	mu     sync.Mutex
	estado map[int64]map[int64][]Estado
}

func NovoLocal() *Local { return &Local{estado: map[int64]map[int64][]Estado{}} }

func (l *Local) Dono(context.Context, int64) (string, error) { return "", nil }
func (l *Local) Reservar(context.Context, int64, string) (bool, string, error) {
	return true, "", nil
}
func (l *Local) Renovar(context.Context, int64, string) {}
func (l *Local) Liberar(context.Context, int64, string) {}

func (l *Local) GravarEstado(_ context.Context, servidorID, canalID int64, estados []Estado) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.estado[servidorID] == nil {
		l.estado[servidorID] = map[int64][]Estado{}
	}
	if len(estados) == 0 {
		delete(l.estado[servidorID], canalID)
	} else {
		l.estado[servidorID][canalID] = estados
	}
	return nil
}

func (l *Local) LerEstado(_ context.Context, servidores []int64) (map[string]map[string][]Estado, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	saida := map[string]map[string][]Estado{}
	for _, s := range servidores {
		if len(l.estado[s]) == 0 {
			continue
		}
		m := map[string][]Estado{}
		for c, e := range l.estado[s] {
			m[strconv.FormatInt(c, 10)] = e
		}
		saida[strconv.FormatInt(s, 10)] = m
	}
	return saida, nil
}

// Redis: várias instâncias.
//
//	prosa:voz:sala:<canal>         endereço da instância com a sala (validade 30 s, renovada)
//	prosa:voz:estado:<servidor>    hash canal -> {"inst": ..., "p": [pessoas]}
type Redis struct {
	Cliente   *redis.Client
	Instancia string
}

const validadeSala = 30 * time.Second

func chaveSala(canal int64) string      { return "prosa:voz:sala:" + strconv.FormatInt(canal, 10) }
func chaveEstado(servidor int64) string { return "prosa:voz:estado:" + strconv.FormatInt(servidor, 10) }
func chaveInstancia(inst string) string { return "prosa:inst:" + inst }

func (r Redis) Dono(ctx context.Context, canal int64) (string, error) {
	v, err := r.Cliente.Get(ctx, chaveSala(canal)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

func (r Redis) Reservar(ctx context.Context, canal int64, endereco string) (bool, string, error) {
	ok, err := r.Cliente.SetNX(ctx, chaveSala(canal), endereco, validadeSala).Result()
	if err != nil {
		return false, "", err
	}
	if ok {
		return true, endereco, nil
	}
	dono, err := r.Dono(ctx, canal)
	if err != nil {
		return false, "", err
	}
	if dono == endereco || dono == "" {
		r.Renovar(ctx, canal, endereco)
		return true, endereco, nil
	}
	return false, dono, nil
}

// renovar só se a reserva ainda for desta instância (script atômico).
var scriptRenovar = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
elseif redis.call("EXISTS", KEYS[1]) == 0 then
  return redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
end
return 0`)

var scriptLiberar = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end
return 0`)

func (r Redis) Renovar(ctx context.Context, canal int64, endereco string) {
	scriptRenovar.Run(ctx, r.Cliente, []string{chaveSala(canal)}, endereco, validadeSala.Milliseconds())
}

func (r Redis) Liberar(ctx context.Context, canal int64, endereco string) {
	scriptLiberar.Run(ctx, r.Cliente, []string{chaveSala(canal)}, endereco)
}

type estadoGuardado struct {
	Instancia string   `json:"inst"`
	Pessoas   []Estado `json:"p"`
}

func (r Redis) GravarEstado(ctx context.Context, servidor, canal int64, estados []Estado) error {
	campo := strconv.FormatInt(canal, 10)
	if len(estados) == 0 {
		return r.Cliente.HDel(ctx, chaveEstado(servidor), campo).Err()
	}
	b, _ := json.Marshal(estadoGuardado{r.Instancia, estados})
	return r.Cliente.HSet(ctx, chaveEstado(servidor), campo, b).Err()
}

func (r Redis) LerEstado(ctx context.Context, servidores []int64) (map[string]map[string][]Estado, error) {
	saida := map[string]map[string][]Estado{}
	if len(servidores) == 0 {
		return saida, nil
	}
	pipe := r.Cliente.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(servidores))
	for i, s := range servidores {
		cmds[i] = pipe.HGetAll(ctx, chaveEstado(s))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	vivas := map[string]bool{}
	for i, c := range cmds {
		for canal, bruto := range c.Val() {
			var e estadoGuardado
			if json.Unmarshal([]byte(bruto), &e) != nil || len(e.Pessoas) == 0 {
				continue
			}
			viva, visto := vivas[e.Instancia]
			if !visto { // instância que caiu sem limpar não conta
				n, _ := r.Cliente.Exists(ctx, chaveInstancia(e.Instancia)).Result()
				viva = n > 0
				vivas[e.Instancia] = viva
			}
			if !viva {
				r.Cliente.HDel(ctx, chaveEstado(servidores[i]), canal)
				continue
			}
			s := strconv.FormatInt(servidores[i], 10)
			if saida[s] == nil {
				saida[s] = map[string][]Estado{}
			}
			saida[s][canal] = e.Pessoas
		}
	}
	return saida, nil
}
