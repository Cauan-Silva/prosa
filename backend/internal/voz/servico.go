package voz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const maxPorSala = 25

// Opcoes configura o servidor de mídia.
type Opcoes struct {
	DB          *sql.DB
	Emissor     *auth.Emissor
	Hub         *eventos.Hub
	Publicador  eventos.Publicador
	Coordenador Coordenador // nil = uma instância só
	Origens     []string
	Endereco    string // ENDERECO_PUBLICO desta instância (para redirecionar a sala certa)
	Instancia   string

	TurnURLs    []string
	TurnUsuario string
	TurnSenha   string
	PortaUDP    int    // 0 = portas aleatórias
	IPPublico   string // anunciado nos candidatos (atrás de NAT/Docker)
}

// ICE é um servidor STUN/TURN no formato que o navegador entende.
type ICE struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type SFU struct {
	o           Opcoes
	api         *webrtc.API
	iceServidor []webrtc.ICEServer
	iceCliente  []ICE
	coord       Coordenador

	mu    sync.Mutex
	salas map[int64]*Sala // canal -> sala
	vigia map[int64]int   // servidor -> salas abertas (para assinar os eventos dele)
}

func Novo(o Opcoes) (*SFU, error) {
	s := &SFU{o: o, salas: map[int64]*Sala{}, vigia: map[int64]int{}, coord: o.Coordenador}
	if s.coord == nil {
		s.coord = NovoLocal()
	}

	ices := []ICE{{URLs: []string{"stun:stun.l.google.com:19302", "stun:stun.cloudflare.com:3478"}}}
	if len(o.TurnURLs) > 0 {
		ices = append(ices, ICE{URLs: o.TurnURLs, Username: o.TurnUsuario, Credential: o.TurnSenha})
	}
	s.iceCliente = ices
	for _, i := range ices {
		s.iceServidor = append(s.iceServidor, webrtc.ICEServer{URLs: i.URLs, Username: i.Username, Credential: i.Credential})
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	reg := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, reg); err != nil {
		return nil, err
	}
	se := webrtc.SettingEngine{}
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	if o.PortaUDP > 0 {
		// Uma porta UDP só para toda a mídia: fácil de liberar no firewall/Docker.
		mux, err := ice.NewMultiUDPMuxFromPort(o.PortaUDP)
		if err != nil {
			return nil, err
		}
		se.SetICEUDPMux(mux)
	}
	if o.IPPublico != "" {
		se.SetNAT1To1IPs([]string{o.IPPublico}, webrtc.ICECandidateTypeHost)
	}
	se.SetICETimeouts(10*time.Second, 25*time.Second, 2*time.Second)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled) // candidatos .local não servem para um servidor
	s.api = webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(reg), webrtc.WithSettingEngine(se))
	return s, nil
}

// ---- informações para o navegador ----

func idDaRota(r *http.Request, nome string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(nome), 10, 64)
	if err != nil || v <= 0 {
		return 0, api.NovoErro(http.StatusNotFound, "Não encontrado.")
	}
	return v, nil
}

// canalDeVoz confere se o canal é de voz e se a pessoa pode entrar nele.
func (s *SFU) canalDeVoz(ctx context.Context, canalID, usuarioID int64) (*acesso.Contexto, error) {
	var servidorID int64
	var tipo string
	err := s.o.DB.QueryRowContext(ctx, `SELECT servidor_id, tipo FROM canais WHERE id = $1`, canalID).Scan(&servidorID, &tipo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, acesso.ErrCanalSumiu
	}
	if err != nil {
		return nil, err
	}
	if tipo != "voz" {
		return nil, api.NovoErro(http.StatusBadRequest, "Este canal não é de voz.")
	}
	c, err := acesso.Carregar(ctx, s.o.DB, servidorID, usuarioID)
	if err != nil {
		return nil, err
	}
	if err := c.ExigirNoCanal(canalID, permissoes.ConectarVoz); err != nil {
		return nil, err
	}
	return c, nil
}

// Info: GET /api/canais/{canal}/voz — onde fica a sala e quais servidores ICE usar.
func (s *SFU) Info(w http.ResponseWriter, r *http.Request) error {
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		return err
	}
	if _, err := s.canalDeVoz(r.Context(), canalID, api.Quem(r).UsuarioID); err != nil {
		return err
	}
	endereco := ""
	if dono, err := s.coord.Dono(r.Context(), canalID); err == nil && dono != "" && dono != s.o.Endereco {
		endereco = dono
	}
	api.Responder(w, http.StatusOK, map[string]any{"endereco": endereco, "ice": s.iceCliente})
	return nil
}

// ---- conexão de voz ----

// Conectar: GET /api/voz/{canal}?t=TOKEN (WebSocket só para a sinalização da voz).
func (s *SFU) Conectar(w http.ResponseWriter, r *http.Request) {
	eu, err := s.o.Emissor.Conferir(r.URL.Query().Get("t"))
	if err != nil {
		http.Error(w, "sessão inválida", http.StatusUnauthorized)
		return
	}
	canalID, err := idDaRota(r, "canal")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	ac, err := s.canalDeVoz(ctx, canalID, eu.UsuarioID)
	if err != nil {
		api.Falhar(w, r, err)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.o.Origens})
	if err != nil {
		return
	}
	ws.SetReadLimit(256 << 10)
	defer ws.CloseNow()

	// A sala fica numa instância só (a mídia de todos passa por ela).
	nossa, dono, err := s.coord.Reservar(ctx, canalID, s.o.Endereco)
	if err != nil {
		ws.Close(websocket.StatusInternalError, "coordenação indisponível")
		return
	}
	if !nossa {
		slog.Info("voz: sala está em outra instância, redirecionando", "canal", canalID, "dono", dono)
		escreverJSON(ctx, ws, mensagem{"VOZ_REDIRECIONAR", map[string]string{"endereco": dono}})
		ws.Close(websocket.StatusNormalClosure, "sala em outra instância")
		return
	}

	sala, err := s.abrirSala(ctx, ac.ServidorID, canalID)
	if err != nil {
		escreverJSON(ctx, ws, mensagem{"VOZ_ERRO", map[string]string{"mensagem": err.Error()}})
		ws.Close(websocket.StatusTryAgainLater, "sala cheia")
		return
	}
	vida, fechar := context.WithCancel(context.Background())
	defer fechar()
	perm := ac.NoCanal(canalID)
	p := &participante{
		sala: sala, ws: ws, envio: make(chan mensagem, 256), fechar: fechar, perm: perm,
		estado: Estado{UsuarioID: eu.UsuarioID, Nome: eu.Nome, Mudo: !perm.Tem(permissoes.Falar)},
	}
	if nome := apelido(ctx, s.o.DB, ac.ServidorID, eu.UsuarioID); nome != "" {
		p.estado.Nome = nome
	}

	go func() { // escrita
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-vida.Done():
				return
			case m := <-p.envio:
				if escreverJSON(vida, ws, m) != nil {
					fechar()
					return
				}
			case <-ping.C:
				c, cancelar := context.WithTimeout(vida, 10*time.Second)
				err := ws.Ping(c)
				cancelar()
				if err != nil {
					fechar()
					return
				}
			}
		}
	}()

	p.mandar("VOZ_PRONTO", map[string]any{"canalId": canalID, "permissoes": uint64(perm), "ice": s.iceCliente})
	if err := sala.entrar(vida, p); err != nil {
		slog.Error("voz: entrar", "erro", err)
		sala.sair(p)
		return
	}
	go func() {
		_ = p.atender(vida)
		fechar()
	}()
	<-vida.Done()
	sala.sair(p)
	ws.Close(websocket.StatusNormalClosure, "")
}

func apelido(ctx context.Context, db *sql.DB, servidorID, usuarioID int64) string {
	var nome string
	_ = db.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(m.apelido, ''), u.nome_exibicao) FROM membros m JOIN usuarios u ON u.id = m.usuario_id
		WHERE m.servidor_id = $1 AND m.usuario_id = $2`, servidorID, usuarioID).Scan(&nome)
	return nome
}

func escreverJSON(ctx context.Context, ws *websocket.Conn, m mensagem) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c, cancelar := context.WithTimeout(ctx, 10*time.Second)
	defer cancelar()
	return ws.Write(c, websocket.MessageText, b)
}

func (s *SFU) abrirSala(ctx context.Context, servidorID, canalID int64) (*Sala, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sala := s.salas[canalID]
	if sala == nil {
		sala = &Sala{sfu: s, ServidorID: servidorID, CanalID: canalID, participantes: map[int64]*participante{}, faixas: map[string]*faixa{}}
		s.salas[canalID] = sala
		if s.vigia[servidorID] == 0 {
			s.o.Hub.Assinar(eventos.TopicoServidor(servidorID), s)
		}
		s.vigia[servidorID]++
	}
	sala.mu.Lock()
	cheia := len(sala.participantes) >= maxPorSala
	sala.mu.Unlock()
	if cheia {
		return nil, errors.New("A sala está cheia (até 25 pessoas).")
	}
	return sala, nil
}

// esvaziou é chamado quando a última pessoa sai.
func (s *SFU) esvaziou(sala *Sala) {
	s.mu.Lock()
	sala.mu.Lock()
	vazia := len(sala.participantes) == 0
	sala.mu.Unlock()
	if !vazia || s.salas[sala.CanalID] != sala {
		s.mu.Unlock()
		return
	}
	delete(s.salas, sala.CanalID)
	s.vigia[sala.ServidorID]--
	if s.vigia[sala.ServidorID] <= 0 {
		delete(s.vigia, sala.ServidorID)
		s.o.Hub.Cancelar(eventos.TopicoServidor(sala.ServidorID), s)
	}
	s.mu.Unlock()
	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelar()
	s.coord.Liberar(ctx, sala.CanalID, s.o.Endereco)
}

// avisar conta para todo mundo do servidor quem está na sala.
func (s *SFU) avisar(ctx context.Context, sala *Sala) {
	estados := sala.estados()
	if err := s.coord.GravarEstado(ctx, sala.ServidorID, sala.CanalID, estados); err != nil {
		slog.Warn("voz: gravar estado", "erro", err)
	}
	e := eventos.Novo(eventos.VozEstado, sala.ServidorID, sala.CanalID, map[string]any{"canalId": sala.CanalID, "participantes": estados})
	e.Exige = uint64(permissoes.VerCanal) // quem não vê o canal não sabe quem está nele
	if err := s.o.Publicador.Publicar(ctx, eventos.TopicoServidor(sala.ServidorID), e); err != nil {
		slog.Warn("voz: publicar estado", "erro", err)
	}
}

// ExtrasPronto põe no PRONTO do gateway quem está em cada sala de voz.
func (s *SFU) ExtrasPronto(ctx context.Context, servidores []int64, podeVer func(servidorID, canalID int64) bool) map[string]any {
	estado, err := s.coord.LerEstado(ctx, servidores)
	if err != nil {
		slog.Warn("voz: ler estado", "erro", err)
		return nil
	}
	for sid, salas := range estado {
		servidorID, _ := strconv.ParseInt(sid, 10, 64)
		for cid := range salas {
			canalID, _ := strconv.ParseInt(cid, 10, 64)
			if !podeVer(servidorID, canalID) {
				delete(salas, cid)
			}
		}
	}
	return map[string]any{"voz": estado}
}

// Manter renova as reservas das salas desta instância enquanto o contexto durar.
func (s *SFU) Manter(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			salas := make([]*Sala, 0, len(s.salas))
			for _, sl := range s.salas {
				salas = append(salas, sl)
			}
			s.mu.Unlock()
			for _, sl := range salas {
				s.coord.Renovar(ctx, sl.CanalID, s.o.Endereco)
				_ = s.coord.GravarEstado(ctx, sl.ServidorID, sl.CanalID, sl.estados())
			}
		}
	}
}

// ---- moderação e permissões ----

func (s *SFU) moderar(ctx context.Context, quem *participante, alvoID int64, acao string) error {
	sala := quem.sala
	ac, err := acesso.Carregar(ctx, s.o.DB, sala.ServidorID, quem.estado.UsuarioID)
	if err != nil {
		return err
	}
	if !ac.NoCanal(sala.CanalID).Tem(permissoes.SilenciarNaVoz) {
		return errors.New("Você não pode moderar esta sala.")
	}
	alvoMembro, err := acesso.CarregarMembro(ctx, s.o.DB, sala.ServidorID, alvoID)
	if err != nil {
		return err
	}
	if alvoID != quem.estado.UsuarioID && !ac.AcimaDe(alvoMembro) {
		return acesso.ErrHierarquia
	}
	sala.mu.Lock()
	alvo := sala.participantes[alvoID]
	sala.mu.Unlock()
	if alvo == nil {
		return errors.New("Essa pessoa não está na sala.")
	}
	switch acao {
	case "silenciar", "liberar":
		alvo.mu.Lock()
		alvo.estado.Silenciado = acao == "silenciar"
		alvo.mu.Unlock()
		s.avisar(ctx, sala)
	case "desconectar":
		alvo.mandar("VOZ_ERRO", map[string]string{"mensagem": "Um moderador tirou você da sala."})
		time.AfterFunc(200*time.Millisecond, alvo.fechar)
	default:
		return errors.New("Ação desconhecida.")
	}
	return nil
}

// Entregar recebe os eventos dos servidores com salas abertas (o SFU assina no Hub)
// para tirar da sala quem saiu do servidor ou perdeu a permissão.
func (s *SFU) Entregar(_ string, e eventos.Evento) {
	if !eventos.MudaPermissoes(e.Tipo) && e.Tipo != eventos.ServidorApagado {
		return
	}
	go s.reconferir(e)
}

func (s *SFU) reconferir(e eventos.Evento) {
	ctx, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	s.mu.Lock()
	var salas []*Sala
	for _, sl := range s.salas {
		if sl.ServidorID == e.ServidorID {
			salas = append(salas, sl)
		}
	}
	s.mu.Unlock()
	for _, sala := range salas {
		sala.mu.Lock()
		ps := make([]*participante, 0, len(sala.participantes))
		for _, p := range sala.participantes {
			ps = append(ps, p)
		}
		sala.mu.Unlock()
		mudou := false
		for _, p := range ps {
			ac, err := acesso.Carregar(ctx, s.o.DB, sala.ServidorID, p.estado.UsuarioID)
			var perm permissoes.Permissao
			if err == nil {
				perm = ac.NoCanal(sala.CanalID)
			} else if !errors.Is(err, acesso.ErrNaoMembro) {
				continue // erro passageiro do banco: deixa como está
			}
			if !perm.Tem(permissoes.ConectarVoz) {
				p.mandar("VOZ_ERRO", map[string]string{"mensagem": "Você não tem mais acesso a esta sala."})
				time.AfterFunc(200*time.Millisecond, p.fechar)
				continue
			}
			p.mu.Lock()
			if p.perm != perm {
				p.perm = perm
				if !perm.Tem(permissoes.Falar) {
					p.estado.Mudo = true
				}
				if !perm.Tem(permissoes.Video) {
					p.estado.Camera, p.estado.Tela = false, false
				}
				mudou = true
			}
			p.mu.Unlock()
		}
		if mudou {
			s.avisar(ctx, sala)
		}
	}
}

// ---- utilidades ----

// IPLocal tenta descobrir o IP desta máquina na rede (só para mensagens de log).
func IPLocal() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	return strings.Split(c.LocalAddr().String(), ":")[0]
}
