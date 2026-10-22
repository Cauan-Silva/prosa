// Package gateway é a conexão de tempo real (WebSocket) entre o navegador e o Prosa.
//
// Ao conectar, a pessoa recebe PRONTO com tudo o que precisa para montar a tela (seus
// servidores, canais visíveis, cargos e quem está online). Depois chegam os eventos dos
// servidores em que ela está, já filtrados pelas permissões: uma mensagem de um canal
// privado nunca sai do servidor para quem não pode ver aquele canal.
package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
	"github.com/Cauan-Silva/prosa/internal/presenca"
	"github.com/Cauan-Silva/prosa/internal/servidores"
	"github.com/Cauan-Silva/prosa/internal/usuarios"
)

const (
	tamanhoFila     = 256
	prazoEscrita    = 10 * time.Second
	intervaloPing   = 25 * time.Second
	intervaloDigita = time.Second
)

// Comando é uma mensagem que o navegador manda pelo gateway (a voz registra os seus).
type Comando func(ctx context.Context, c *Conexao, dados json.RawMessage) error

type Gateway struct {
	DB         *sql.DB
	Emissor    *auth.Emissor
	Hub        *eventos.Hub
	Publicador eventos.Publicador
	Presenca   presenca.Registro
	Servidores *servidores.Servico
	Usuarios   *usuarios.Servico
	Origens    []string
	Instancia  string

	// ExtrasPronto acrescenta dados ao PRONTO (a voz manda quem está em cada sala).
	// podeVer diz se a pessoa vê o canal (para não revelar salas privadas).
	ExtrasPronto func(ctx context.Context, servidores []int64, podeVer func(servidorID, canalID int64) bool) map[string]any
	// AoDesconectar avisa outros módulos (a voz tira a pessoa da sala).
	AoDesconectar func(c *Conexao)

	mu       sync.RWMutex
	comandos map[string]Comando
	abertas  atomic.Int64
}

// Registrar adiciona um comando aceito pelo gateway.
func (g *Gateway) Registrar(tipo string, cmd Comando) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.comandos == nil {
		g.comandos = map[string]Comando{}
	}
	g.comandos[tipo] = cmd
}

// Abertas diz quantas conexões esta instância tem (para /api/saude e o teste de carga).
func (g *Gateway) Abertas() int64 { return g.abertas.Load() }

// saida é o formato que vai para o navegador.
type saida struct {
	Tipo       string `json:"t"`
	ServidorID int64  `json:"s,omitempty"`
	CanalID    int64  `json:"c,omitempty"`
	UsuarioID  int64  `json:"u,omitempty"`
	Dados      any    `json:"d,omitempty"`
	pronto     []byte // já codificado (eventos do Hub)
}

// Conexao é uma aba aberta de alguém.
type Conexao struct {
	g        *Gateway
	ws       *websocket.Conn
	Usuario  auth.Identidade
	ID       string
	fila     chan eventos.Evento
	diretas  chan saida
	cancelar context.CancelFunc
	lenta    atomic.Bool

	// só a goroutine de escrita mexe nestes
	servidores map[int64]*acesso.Contexto
	assinatura map[int64]string

	digitou sync.Map // canalID -> time.Time
}

var proximoID atomic.Int64

// Entregar recebe um evento do Hub. Nunca bloqueia: se a fila encher (conexão lenta
// demais), a conexão é fechada e o navegador reconecta e sincroniza de novo.
func (c *Conexao) Entregar(_ string, e eventos.Evento) {
	select {
	case c.fila <- e:
	default:
		if c.lenta.CompareAndSwap(false, true) {
			c.cancelar()
		}
	}
}

// Enviar manda algo só para esta conexão (respostas de comandos, voz).
func (c *Conexao) Enviar(tipo string, servidorID int64, dados any) {
	select {
	case c.diretas <- saida{Tipo: tipo, ServidorID: servidorID, Dados: dados}:
	default:
		if c.lenta.CompareAndSwap(false, true) {
			c.cancelar()
		}
	}
}

// Contexto carrega as permissões atuais da pessoa no servidor (direto do banco).
func (c *Conexao) Contexto(ctx context.Context, servidorID int64) (*acesso.Contexto, error) {
	return acesso.Carregar(ctx, c.g.DB, servidorID, c.Usuario.UsuarioID)
}

// ServeHTTP: GET /api/gateway?t=TOKEN (o navegador não manda cabeçalhos no WebSocket).
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if token == "" {
		token, _ = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	eu, err := g.Emissor.Conferir(token)
	if err != nil {
		http.Error(w, "sessão inválida", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: g.Origens, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 10)
	ctx, cancelar := context.WithCancel(context.Background())
	c := &Conexao{
		g: g, ws: ws, Usuario: eu, cancelar: cancelar,
		ID:         g.Instancia + ":" + strconv.FormatInt(proximoID.Add(1), 10),
		fila:       make(chan eventos.Evento, tamanhoFila),
		diretas:    make(chan saida, 64),
		servidores: map[int64]*acesso.Contexto{},
		assinatura: map[int64]string{},
	}
	g.abertas.Add(1)
	defer g.abertas.Add(-1)
	g.rodar(ctx, c)
	cancelar()
	if c.lenta.Load() {
		ws.Close(websocket.StatusPolicyViolation, "conexão lenta demais")
	} else {
		ws.Close(websocket.StatusNormalClosure, "")
	}
}

func (g *Gateway) rodar(ctx context.Context, c *Conexao) {
	eu := c.Usuario.UsuarioID
	// Assina antes de montar o PRONTO para não perder nada no meio (eventos que chegarem
	// enquanto isso ficam na fila; o navegador ignora o que já tem).
	g.Hub.Assinar(eventos.TopicoUsuario(eu), c)
	ids, err := g.Servidores.ServidoresDe(ctx, eu)
	if err != nil {
		slog.Error("gateway: servidores", "erro", err)
		g.Hub.Cancelar(eventos.TopicoUsuario(eu), c)
		return
	}
	topicos := []string{eventos.TopicoUsuario(eu)}
	for _, id := range ids {
		g.Hub.Assinar(eventos.TopicoServidor(id), c)
		c.servidores[id] = nil
		topicos = append(topicos, eventos.TopicoServidor(id))
	}
	defer func() {
		g.Hub.Cancelar(eventos.TopicoUsuario(eu), c)
		for id := range c.servidores {
			g.Hub.Cancelar(eventos.TopicoServidor(id), c)
		}
		if g.AoDesconectar != nil {
			g.AoDesconectar(c)
		}
		desligar, pronto := context.WithTimeout(context.Background(), 5*time.Second)
		defer pronto()
		if ultima, err := g.Presenca.Desconectou(desligar, eu); err == nil && ultima {
			g.avisarPresenca(desligar, c, false)
		}
	}()

	if err := g.Hub.Aguardar(ctx, topicos...); err != nil {
		slog.Warn("gateway: assinatura", "erro", err)
	}
	primeira, err := g.Presenca.Conectou(ctx, eu)
	if err != nil {
		slog.Warn("gateway: presença", "erro", err)
	}
	pronto, err := g.montarPronto(ctx, c)
	if err != nil {
		slog.Error("gateway: pronto", "erro", err)
		return
	}
	if err := c.escrever(ctx, saida{Tipo: eventos.Pronto, Dados: pronto}); err != nil {
		return
	}
	if primeira {
		g.avisarPresenca(ctx, c, true)
	}

	go c.ler(ctx)
	c.escreverEventos(ctx)
}

func (g *Gateway) avisarPresenca(ctx context.Context, c *Conexao, online bool) {
	dados := map[string]any{"usuarioId": c.Usuario.UsuarioID, "online": online}
	for id := range c.servidores {
		e := eventos.Novo(eventos.Presenca, id, 0, dados)
		if err := g.Publicador.Publicar(ctx, eventos.TopicoServidor(id), e); err != nil {
			slog.Warn("gateway: presença", "erro", err)
		}
	}
}

type pronto struct {
	Usuario    usuarios.Usuario           `json:"usuario"`
	Servidores []servidores.VisaoServidor `json:"servidores"`
	Online     map[string][]int64         `json:"online"` // servidor -> pessoas online
	Instancia  string                     `json:"instancia"`
	Extras     map[string]any             `json:"extras,omitempty"`
}

func (g *Gateway) montarPronto(ctx context.Context, c *Conexao) (pronto, error) {
	eu := c.Usuario.UsuarioID
	p := pronto{Servidores: []servidores.VisaoServidor{}, Online: map[string][]int64{}, Instancia: g.Instancia}
	var err error
	if p.Usuario, err = g.Usuarios.Buscar(ctx, eu); err != nil {
		return p, err
	}
	ids := make([]int64, 0, len(c.servidores))
	for id := range c.servidores {
		v, err := g.Servidores.Visao(ctx, id, eu)
		if errors.Is(err, acesso.ErrNaoMembro) {
			continue // saiu entre uma consulta e outra
		}
		if err != nil {
			return p, err
		}
		p.Servidores = append(p.Servidores, v)
		ids = append(ids, id)
		if ac, err := c.Contexto(ctx, id); err == nil {
			c.servidores[id] = ac
			c.assinatura[id] = assinatura(ac)
		}
	}
	sort.Slice(p.Servidores, func(i, j int) bool { return p.Servidores[i].Servidor.ID < p.Servidores[j].Servidor.ID })

	// quem está online em cada servidor
	if len(ids) > 0 {
		linhas, err := g.DB.QueryContext(ctx, `SELECT servidor_id, usuario_id FROM membros WHERE servidor_id = ANY($1)`, pq.Array(ids))
		if err != nil {
			return p, err
		}
		porServidor := map[int64][]int64{}
		todos := map[int64]bool{}
		for linhas.Next() {
			var s, u int64
			if err := linhas.Scan(&s, &u); err != nil {
				linhas.Close()
				return p, err
			}
			porServidor[s] = append(porServidor[s], u)
			todos[u] = true
		}
		linhas.Close()
		lista := make([]int64, 0, len(todos))
		for u := range todos {
			lista = append(lista, u)
		}
		online, err := g.Presenca.Online(ctx, lista)
		if err != nil {
			slog.Warn("gateway: presença", "erro", err)
			online = map[int64]bool{}
		}
		online[eu] = true
		for s, membros := range porServidor {
			on := []int64{}
			for _, u := range membros {
				if online[u] {
					on = append(on, u)
				}
			}
			p.Online[strconv.FormatInt(s, 10)] = on
		}
	}
	if g.ExtrasPronto != nil {
		p.Extras = g.ExtrasPronto(ctx, ids, func(servidorID, canalID int64) bool {
			ac := c.servidores[servidorID]
			return ac != nil && ac.NoCanal(canalID).Tem(permissoes.VerCanal)
		})
	}
	return p, nil
}

// assinatura resume o que a pessoa pode fazer no servidor; se mudar, o navegador recarrega
// a visão do servidor (canais que apareceram ou sumiram, botões de moderação...).
func assinatura(c *acesso.Contexto) string {
	ids := make([]int64, 0, len(c.Canais))
	for id := range c.Canais {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	b.WriteString(strconv.FormatUint(uint64(c.Base()), 36))
	for _, id := range ids {
		if p := c.NoCanal(id); p != 0 {
			b.WriteString("|" + strconv.FormatInt(id, 36) + ":" + strconv.FormatUint(uint64(p), 36))
		}
	}
	return b.String()
}

func (c *Conexao) escrever(ctx context.Context, s saida) error {
	dados := s.pronto
	if dados == nil {
		var err error
		if dados, err = json.Marshal(s); err != nil {
			return err
		}
	}
	prazo, cancelar := context.WithTimeout(ctx, prazoEscrita)
	defer cancelar()
	return c.ws.Write(prazo, websocket.MessageText, dados)
}

func (c *Conexao) escreverEventos(ctx context.Context) {
	ping := time.NewTicker(intervaloPing)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			prazo, cancelar := context.WithTimeout(ctx, prazoEscrita)
			err := c.ws.Ping(prazo)
			cancelar()
			if err != nil {
				c.cancelar()
				return
			}
		case s := <-c.diretas:
			if err := c.escrever(ctx, s); err != nil {
				c.cancelar()
				return
			}
		case e := <-c.fila:
			for _, s := range c.processar(ctx, e) {
				if err := c.escrever(ctx, s); err != nil {
					c.cancelar()
					return
				}
			}
		}
	}
}

// processar decide o que um evento vira para esta conexão (pode virar nada).
func (c *Conexao) processar(ctx context.Context, e eventos.Evento) []saida {
	g, eu := c.g, c.Usuario.UsuarioID
	bruto := saida{Tipo: e.Tipo, ServidorID: e.ServidorID, CanalID: e.CanalID, UsuarioID: e.UsuarioID, pronto: e.Pronto}
	if len(e.Dados) > 0 {
		bruto.Dados = e.Dados
	}

	switch {
	case e.Tipo == eventos.ServidorCriado && e.UsuarioID == eu:
		// entrou num servidor (criou ou aceitou convite) em qualquer aba/instância
		if _, ja := c.servidores[e.ServidorID]; ja {
			return nil
		}
		v, err := g.Servidores.Visao(ctx, e.ServidorID, eu)
		if err != nil {
			return nil
		}
		g.Hub.Assinar(eventos.TopicoServidor(e.ServidorID), c)
		_ = g.Hub.Aguardar(ctx, eventos.TopicoServidor(e.ServidorID))
		c.servidores[e.ServidorID] = nil
		if ac, err := c.Contexto(ctx, e.ServidorID); err == nil {
			c.servidores[e.ServidorID], c.assinatura[e.ServidorID] = ac, assinatura(ac)
		}
		// quem já estava no servidor fica sabendo que a pessoa nova está online
		pres := eventos.Novo(eventos.Presenca, e.ServidorID, 0, map[string]any{"usuarioId": eu, "online": true})
		if err := g.Publicador.Publicar(ctx, eventos.TopicoServidor(e.ServidorID), pres); err != nil {
			slog.Warn("gateway: presença", "erro", err)
		}
		online := []int64{}
		if mapa, err := g.Presenca.Online(ctx, membrosDaVisao(ctx, g.DB, e.ServidorID)); err == nil {
			for u := range mapa {
				online = append(online, u)
			}
		}
		return []saida{{Tipo: eventos.ServidorCriado, ServidorID: e.ServidorID, Dados: map[string]any{"visao": v, "online": online}}}

	case e.Tipo == eventos.ServidorApagado, e.Tipo == eventos.MembroSaiu && e.UsuarioID == eu:
		if _, ja := c.servidores[e.ServidorID]; !ja {
			return nil
		}
		g.Hub.Cancelar(eventos.TopicoServidor(e.ServidorID), c)
		delete(c.servidores, e.ServidorID)
		delete(c.assinatura, e.ServidorID)
		return []saida{{Tipo: eventos.ServidorApagado, ServidorID: e.ServidorID}}
	}

	if e.ServidorID == 0 || strings.HasPrefix(e.Tipo, "VOZ_SINAL") {
		return []saida{bruto}
	}
	if _, membro := c.servidores[e.ServidorID]; !membro {
		return nil
	}

	var extra []saida
	if eventos.MudaPermissoes(e.Tipo) || c.servidores[e.ServidorID] == nil {
		ac, err := c.Contexto(ctx, e.ServidorID)
		if err != nil {
			return nil
		}
		c.servidores[e.ServidorID] = ac
		if nova := assinatura(ac); nova != c.assinatura[e.ServidorID] {
			c.assinatura[e.ServidorID] = nova
			extra = append(extra, saida{Tipo: "PERMISSOES_ALTERADAS", ServidorID: e.ServidorID})
		}
	}
	ac := c.servidores[e.ServidorID]

	exige := permissoes.Permissao(e.Exige)
	if e.Tipo == eventos.CanalCriado || e.Tipo == eventos.CanalAlterado {
		exige |= permissoes.VerCanal // não revelar o nome de canais privados
	}
	if exige != 0 {
		var tem permissoes.Permissao
		if e.CanalID != 0 {
			tem = ac.NoCanal(e.CanalID)
		} else {
			tem = ac.Base()
		}
		if tem&exige != exige {
			return extra
		}
	}
	return append([]saida{bruto}, extra...)
}

func membrosDaVisao(ctx context.Context, db *sql.DB, servidorID int64) []int64 {
	var ids pq.Int64Array
	_ = db.QueryRowContext(ctx, `SELECT array_agg(usuario_id) FROM membros WHERE servidor_id = $1`, servidorID).Scan(&ids)
	return ids
}

type entrada struct {
	Tipo  string          `json:"t"`
	Dados json.RawMessage `json:"d"`
}

func (c *Conexao) ler(ctx context.Context) {
	defer c.cancelar()
	for {
		tipo, dados, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		if tipo != websocket.MessageText {
			continue
		}
		var m entrada
		if json.Unmarshal(dados, &m) != nil {
			continue
		}
		if err := c.comando(ctx, m); err != nil {
			c.Enviar("ERRO", 0, map[string]string{"comando": m.Tipo, "erro": err.Error()})
		}
	}
}

func (c *Conexao) comando(ctx context.Context, m entrada) error {
	switch m.Tipo {
	case "DIGITANDO":
		var p struct {
			CanalID int64 `json:"canalId"`
		}
		if json.Unmarshal(m.Dados, &p) != nil || p.CanalID <= 0 {
			return nil
		}
		return c.digitando(ctx, p.CanalID)
	case "PING":
		c.Enviar("PONG", 0, nil)
		return nil
	}
	c.g.mu.RLock()
	cmd := c.g.comandos[m.Tipo]
	c.g.mu.RUnlock()
	if cmd == nil {
		return errors.New("comando desconhecido")
	}
	return cmd(ctx, c, m.Dados)
}

func (c *Conexao) digitando(ctx context.Context, canalID int64) error {
	agora := time.Now()
	if v, ok := c.digitou.Load(canalID); ok && agora.Sub(v.(time.Time)) < intervaloDigita {
		return nil
	}
	c.digitou.Store(canalID, agora)
	var servidorID int64
	if err := c.g.DB.QueryRowContext(ctx, `SELECT servidor_id FROM canais WHERE id = $1 AND tipo = 'texto'`, canalID).Scan(&servidorID); err != nil {
		return nil
	}
	ac, err := c.Contexto(ctx, servidorID)
	if err != nil || !ac.NoCanal(canalID).Tem(permissoes.EnviarMensagens) {
		return nil
	}
	e := eventos.Novo(eventos.Digitando, servidorID, canalID, map[string]any{"usuarioId": c.Usuario.UsuarioID, "nome": c.Usuario.Nome})
	e.Exige = uint64(permissoes.VerCanal)
	return c.g.Publicador.Publicar(ctx, eventos.TopicoServidor(servidorID), e)
}
