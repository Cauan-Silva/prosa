// Comando carga: teste de carga do Prosa.
//
// Cria um servidor, cadastra N pessoas, conecta todas no tempo real e coloca algumas
// para mandar mensagens num ritmo fixo. Mede quanto tempo cada mensagem leva desde o
// envio até chegar em cada conexão (latência de entrega) e quantas se perderam.
//
//	go run ./cmd/carga -url http://localhost:8080 -usuarios 300 -remetentes 30 -taxa 1 -duracao 30s
//
// Com várias instâncias, passe todas em -url separadas por vírgula (as conexões são
// distribuídas entre elas) ou o endereço do balanceador. O servidor precisa aceitar
// muitos cadastros do mesmo IP: rode com LOGIN_POR_MINUTO=100000.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

var (
	enderecos  = flag.String("url", "http://localhost:8080", "endereço do Prosa (vários separados por vírgula)")
	usuarios   = flag.Int("usuarios", 100, "pessoas conectadas")
	remetentes = flag.Int("remetentes", 10, "quantas delas mandam mensagens")
	taxa       = flag.Float64("taxa", 1, "mensagens por segundo de cada remetente (o limite do servidor é 8 a cada 5 s)")
	duracao    = flag.Duration("duracao", 30*time.Second, "tempo mandando mensagens")
	paralelo   = flag.Int("paralelo", 20, "cadastros/conexões ao mesmo tempo")
)

type pessoa struct {
	token string
	id    int64
	base  string
}

var cliente = &http.Client{Timeout: 30 * time.Second}

func pedir(base, token, metodo, caminho string, corpo, saida any) (int, error) {
	var leitor io.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		leitor = bytes.NewReader(b)
	}
	req, err := http.NewRequest(metodo, base+caminho, leitor)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cliente.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", metodo, caminho, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if saida != nil {
		return resp.StatusCode, json.Unmarshal(b, saida)
	}
	return resp.StatusCode, nil
}

func cadastrar(base, prefixo string, i int) (pessoa, error) {
	nome := fmt.Sprintf("%s%d", prefixo, i)
	var r struct {
		Token   string `json:"token"`
		Usuario struct {
			ID int64 `json:"id"`
		} `json:"usuario"`
	}
	_, err := pedir(base, "", "POST", "/api/cadastro", map[string]string{
		"nomeUsuario": nome, "nomeExibicao": "Carga " + strconv.Itoa(i), "email": nome + "@carga.teste", "senha": "senha-da-carga-123",
	}, &r)
	return pessoa{token: r.Token, id: r.Usuario.ID, base: base}, err
}

// medidas guardadas por todos os leitores
type medidas struct {
	mu        sync.Mutex
	latencias []time.Duration
	recebidas atomic.Int64
	enviadas  atomic.Int64
	falhas    sync.Map // status -> *atomic.Int64
	quedas    atomic.Int64
}

func (m *medidas) falhou(motivo string) {
	v, _ := m.falhas.LoadOrStore(motivo, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func main() {
	flag.Parse()
	bases := strings.Split(*enderecos, ",")
	for i := range bases {
		bases[i] = strings.TrimRight(strings.TrimSpace(bases[i]), "/")
	}
	prefixo := fmt.Sprintf("c%x", rand.Uint32()&0xffff)
	fmt.Printf("Prosa carga: %d pessoas, %d remetentes a %.1f msg/s por %s em %s\n", *usuarios, *remetentes, *taxa, *duracao, strings.Join(bases, ", "))

	// 1. dono, servidor e convite
	dono, err := cadastrar(bases[0], prefixo+"dono", 0)
	sair(err)
	var visao struct {
		Servidor struct {
			ID int64 `json:"id"`
		} `json:"servidor"`
		Canais []struct {
			ID   int64  `json:"id"`
			Tipo string `json:"tipo"`
		} `json:"canais"`
	}
	_, err = pedir(dono.base, dono.token, "POST", "/api/servidores", map[string]string{"nome": "Carga " + prefixo}, &visao)
	sair(err)
	var canal int64
	for _, c := range visao.Canais {
		if c.Tipo == "texto" {
			canal = c.ID
		}
	}
	var convite struct {
		Codigo string `json:"codigo"`
	}
	_, err = pedir(dono.base, dono.token, "POST", fmt.Sprintf("/api/servidores/%d/canais/%d/convites", visao.Servidor.ID, canal), map[string]int{}, &convite)
	sair(err)

	// 2. cadastros e convites aceitos
	inicio := time.Now()
	pessoas := make([]pessoa, *usuarios)
	var erros atomic.Int64
	vaga := make(chan struct{}, *paralelo)
	var wg sync.WaitGroup
	for i := range pessoas {
		wg.Add(1)
		vaga <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-vaga }()
			p, err := cadastrar(bases[i%len(bases)], prefixo, i+1)
			if err == nil {
				_, err = pedir(p.base, p.token, "POST", "/api/convites/"+convite.Codigo, nil, nil)
			}
			if err != nil {
				if erros.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "  erro:", err)
				}
				return
			}
			pessoas[i] = p
		}(i)
	}
	wg.Wait()
	fmt.Printf("Cadastros: %d em %s (%d erros)\n", *usuarios-int(erros.Load()), time.Since(inicio).Round(time.Millisecond), erros.Load())

	// 3. conexões de tempo real
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	m := &medidas{}
	var conectadas atomic.Int64
	tempos := make([]time.Duration, 0, *usuarios)
	var muTempos sync.Mutex
	inicio = time.Now()
	for _, p := range pessoas {
		if p.token == "" {
			continue
		}
		wg.Add(1)
		vaga <- struct{}{}
		go func(p pessoa) {
			defer wg.Done()
			t0 := time.Now()
			ws, err := conectar(ctx, p)
			<-vaga
			if err != nil {
				m.falhou("conexão: " + err.Error())
				return
			}
			muTempos.Lock()
			tempos = append(tempos, time.Since(t0))
			muTempos.Unlock()
			conectadas.Add(1)
			go ler(ctx, ws, m)
		}(p)
	}
	wg.Wait()
	fmt.Printf("Conexões: %d em %s (até PRONTO: p50 %s, p95 %s)\n", conectadas.Load(), time.Since(inicio).Round(time.Millisecond),
		percentil(tempos, 50), percentil(tempos, 95))

	// 4. mensagens
	fmt.Printf("Mandando mensagens por %s…\n", *duracao)
	fim := time.Now().Add(*duracao)
	intervalo := time.Duration(float64(time.Second) / *taxa)
	var envios sync.WaitGroup
	enviadores := 0
	for _, p := range pessoas {
		if enviadores == *remetentes {
			break
		}
		if p.token == "" {
			continue
		}
		enviadores++
		envios.Add(1)
		go func(p pessoa) {
			defer envios.Done()
			time.Sleep(time.Duration(rand.Int64N(int64(intervalo)))) // espalha o começo
			t := time.NewTicker(intervalo)
			defer t.Stop()
			for time.Now().Before(fim) {
				conteudo := "carga " + strconv.FormatInt(time.Now().UnixNano(), 10)
				status, err := pedir(p.base, p.token, "POST", fmt.Sprintf("/api/canais/%d/mensagens", canal), map[string]string{"conteudo": conteudo}, nil)
				if err != nil {
					m.falhou("envio " + strconv.Itoa(status))
				} else {
					m.enviadas.Add(1)
				}
				<-t.C
			}
		}(p)
	}
	envios.Wait()
	time.Sleep(3 * time.Second) // espera as últimas chegarem

	// 5. relatório
	m.mu.Lock()
	lat := append([]time.Duration(nil), m.latencias...)
	m.mu.Unlock()
	esperadas := m.enviadas.Load() * conectadas.Load()
	fmt.Println()
	fmt.Println("Resultado")
	fmt.Printf("  mensagens enviadas:   %d (%.1f/s)\n", m.enviadas.Load(), float64(m.enviadas.Load())/duracao.Seconds())
	fmt.Printf("  entregas esperadas:   %d\n", esperadas)
	fmt.Printf("  entregas recebidas:   %d (%.2f%%)\n", m.recebidas.Load(), 100*float64(m.recebidas.Load())/float64(max(esperadas, 1)))
	fmt.Printf("  entregas por segundo: %.0f\n", float64(m.recebidas.Load())/duracao.Seconds())
	fmt.Printf("  latência de entrega:  p50 %s  p95 %s  p99 %s  máx %s\n", percentil(lat, 50), percentil(lat, 95), percentil(lat, 99), percentil(lat, 100))
	fmt.Printf("  conexões que caíram:  %d\n", m.quedas.Load())
	m.falhas.Range(func(k, v any) bool {
		fmt.Printf("  falha %q: %d\n", k, v.(*atomic.Int64).Load())
		return true
	})
}

func conectar(ctx context.Context, p pessoa) (*websocket.Conn, error) {
	endereco := "ws" + strings.TrimPrefix(p.base, "http") + "/api/gateway?t=" + p.token
	c, cancelar := context.WithTimeout(ctx, 20*time.Second)
	defer cancelar()
	ws, _, err := websocket.Dial(c, endereco, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(4 << 20)
	for { // espera o PRONTO
		_, b, err := ws.Read(c)
		if err != nil {
			return nil, err
		}
		if bytes.HasPrefix(b, []byte(`{"t":"PRONTO"`)) {
			return ws, nil
		}
	}
}

func ler(ctx context.Context, ws *websocket.Conn, m *medidas) {
	defer ws.CloseNow()
	for {
		_, b, err := ws.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				m.quedas.Add(1)
			}
			return
		}
		agora := time.Now()
		if !bytes.HasPrefix(b, []byte(`{"t":"MENSAGEM_CRIADA"`)) {
			continue
		}
		var e struct {
			D struct {
				Conteudo string `json:"conteudo"`
			} `json:"d"`
		}
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		ns, err := strconv.ParseInt(strings.TrimPrefix(e.D.Conteudo, "carga "), 10, 64)
		if err != nil {
			continue
		}
		m.recebidas.Add(1)
		m.mu.Lock()
		m.latencias = append(m.latencias, agora.Sub(time.Unix(0, ns)))
		m.mu.Unlock()
	}
}

func percentil(lista []time.Duration, p float64) time.Duration {
	if len(lista) == 0 {
		return 0
	}
	sort.Slice(lista, func(i, j int) bool { return lista[i] < lista[j] })
	i := int(float64(len(lista)-1) * p / 100)
	return lista[i].Round(100 * time.Microsecond)
}

func sair(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}
