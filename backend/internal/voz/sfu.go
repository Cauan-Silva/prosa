// Package voz é o servidor de mídia (SFU) dos canais de voz, feito com o Pion.
//
// Cada pessoa abre uma única conexão WebRTC com o servidor e manda até três faixas
// (microfone, câmera e tela). O servidor repassa os pacotes de cada faixa para todo
// mundo na sala, sem decodificar nada: é um "roteador" de mídia (SFU), então cada
// navegador envia uma vez só, não importa quantas pessoas estejam ouvindo.
//
// A negociação é sempre puxada pelo servidor: ele manda a oferta (VOZ_OFERTA) e o
// navegador só responde. Quando alguém liga a câmera ou entra na sala, o servidor
// acrescenta as faixas novas nas conexões dos outros e manda uma oferta nova.
//
// Salas pequenas (até Opcoes.MaxDireto pessoas) usam o modo "direto": a mídia vai de
// navegador para navegador e o servidor só repassa a negociação (VOZ_P2P). Assim o
// áudio e o vídeo não passam pela hospedagem (que cobra o tráfego de saída). Quando a
// sala passa do limite, todo mundo troca para o modo "sfu"; quando esvazia, volta.
package voz

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Tipos das faixas que cada pessoa pode mandar, na ordem dos transceptores.
// "telaAudio" é o som da tela compartilhada (vídeo, jogo, música).
var tiposFaixa = []string{"audio", "camera", "tela", "telaAudio"}

// Estado é o que os outros veem de quem está na sala.
type Estado struct {
	UsuarioID  int64  `json:"usuarioId"`
	Nome       string `json:"nome"`
	Mudo       bool   `json:"mudo"`
	Surdo      bool   `json:"surdo"`
	Camera     bool   `json:"camera"`
	Tela       bool   `json:"tela"`
	Silenciado bool   `json:"silenciado"` // silenciado por um moderador
	// Sem permissão de falar / de vídeo. No modo direto quem recebe é que corta a mídia
	// (o servidor não está no caminho), então precisa saber.
	SemVoz   bool `json:"semVoz,omitempty"`
	SemVideo bool `json:"semVideo,omitempty"`
}

const (
	modoDireto = "direto"
	modoSFU    = "sfu"
)

type mensagem struct {
	Tipo  string `json:"t"`
	Dados any    `json:"d,omitempty"`
}

// faixa é uma faixa recebida de alguém e repassada aos outros.
type faixa struct {
	dono   *participante
	pc     *webrtc.PeerConnection // conexão do dono por onde a faixa chega
	tipo   string
	local  *webrtc.TrackLocalStaticRTP
	remota *webrtc.TrackRemote
}

func (f *faixa) chave() string { return strconv.FormatInt(f.dono.estado.UsuarioID, 10) + ":" + f.tipo }

type participante struct {
	sala   *Sala
	pc     *webrtc.PeerConnection
	ws     *websocket.Conn
	envio  chan mensagem
	fechar context.CancelFunc

	mu         sync.Mutex
	estado     Estado
	perm       permissoes.Permissao
	geracao    int                          // muda a cada conexão nova com o servidor
	mids       map[string]string            // mid do transceptor de envio -> tipo
	saidas     map[string]*webrtc.RTPSender // faixa (chave) -> envio nesta conexão
	negociando bool
	pendente   bool
}

func (p *participante) mandar(t string, d any) {
	select {
	case p.envio <- mensagem{t, d}:
	default:
		p.fechar() // fila cheia: conexão travada
	}
}

// conexao devolve a conexão atual com o servidor (nil no modo direto) e a geração dela.
func (p *participante) conexao() (*webrtc.PeerConnection, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pc, p.geracao
}

// Sala é um canal de voz com gente dentro (nesta instância).
type Sala struct {
	sfu        *SFU
	ServidorID int64
	CanalID    int64

	ordem sync.Mutex // uma entrada, saída ou troca de modo de cada vez

	mu            sync.Mutex
	modo          string
	participantes map[int64]*participante
	faixas        map[string]*faixa
}

func (s *Sala) estados() []Estado {
	lista, _, _ := s.retrato()
	return lista
}

// retrato devolve, de uma vez, quem está na sala, o modo e as conexões de voz.
func (s *Sala) retrato() ([]Estado, string, []*participante) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lista := make([]Estado, 0, len(s.participantes))
	ps := make([]*participante, 0, len(s.participantes))
	for _, p := range s.participantes {
		p.mu.Lock()
		e := p.estado
		e.SemVoz = !p.perm.Tem(permissoes.Falar)
		e.SemVideo = !p.perm.Tem(permissoes.Video)
		p.mu.Unlock()
		lista = append(lista, e)
		ps = append(ps, p)
	}
	return lista, s.modo, ps
}

// entrar coloca a pessoa na sala. No modo sfu já abre a conexão de mídia com o servidor;
// se a sala passou do limite do modo direto, troca todo mundo para o sfu.
func (s *Sala) entrar(ctx context.Context, p *participante) error {
	s.ordem.Lock()
	defer s.ordem.Unlock()

	s.mu.Lock()
	if antigo := s.participantes[p.estado.UsuarioID]; antigo != nil {
		antigo.fechar() // entrou de novo (outra aba): a sessão antiga sai
	}
	s.participantes[p.estado.UsuarioID] = p
	total := len(s.participantes)
	modo := s.modo
	s.mu.Unlock()

	max := s.sfu.o.MaxDireto
	switch {
	case modo == modoDireto && total > max:
		s.trocarModo(modoSFU)
	case modo == modoSFU:
		if err := p.iniciarMidia(ctx); err != nil {
			return err
		}
	}
	s.sfu.avisar(ctx, s)
	return nil
}

// trocarModo passa a sala inteira para o modo direto ou sfu (chamado com s.ordem travada).
func (s *Sala) trocarModo(modo string) {
	s.mu.Lock()
	if s.modo == modo {
		s.mu.Unlock()
		return
	}
	s.modo = modo
	s.faixas = map[string]*faixa{}
	ps := make([]*participante, 0, len(s.participantes))
	for _, p := range s.participantes {
		ps = append(ps, p)
	}
	s.mu.Unlock()
	slog.Info("voz: sala trocou de modo", "canal", s.CanalID, "modo", modo, "pessoas", len(ps))

	for _, p := range ps {
		if modo == modoDireto {
			p.pararMidia()
		} else if err := p.iniciarMidia(context.Background()); err != nil {
			slog.Warn("voz: abrir mídia", "erro", err)
			p.fechar()
		}
	}
}

// iniciarMidia abre a conexão WebRTC da pessoa com o servidor e manda a primeira oferta.
func (p *participante) iniciarMidia(ctx context.Context) error {
	s := p.sala
	c, cancelar := context.WithTimeout(ctx, 5*time.Second)
	ices := s.sfu.servidoresICE(c)
	cancelar()
	pc, err := s.sfu.api.NewPeerConnection(webrtc.Configuration{ICEServers: paraPion(ices)})
	if err != nil {
		return err
	}

	// Um transceptor para cada coisa que a pessoa pode mandar: microfone, câmera, tela e som da tela.
	for _, tipo := range tiposFaixa {
		tipoMidia := webrtc.RTPCodecTypeVideo
		if tipo == "audio" || tipo == "telaAudio" {
			tipoMidia = webrtc.RTPCodecTypeAudio
		}
		if _, err := pc.AddTransceiverFromKind(tipoMidia, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			pc.Close()
			return err
		}
	}

	p.mu.Lock()
	antiga := p.pc
	p.pc = pc
	p.geracao++
	g := p.geracao
	p.mids = map[string]string{}
	p.saidas = map[string]*webrtc.RTPSender{}
	p.negociando, p.pendente = false, false
	p.mu.Unlock()
	if antiga != nil {
		go antiga.Close()
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			p.mandar("VOZ_ICE", map[string]any{"candidato": c.ToJSON(), "g": g})
		}
	})
	pc.OnConnectionStateChange(func(estado webrtc.PeerConnectionState) {
		if estado != webrtc.PeerConnectionStateFailed && estado != webrtc.PeerConnectionStateClosed {
			return
		}
		if atual, _ := p.conexao(); atual == pc {
			p.fechar() // a conexão atual caiu (uma antiga fechando é normal na troca de modo)
		}
	})
	pc.OnTrack(func(remota *webrtc.TrackRemote, receptor *webrtc.RTPReceiver) {
		tipo := ""
		for _, t := range pc.GetTransceivers() {
			if t.Receiver() == receptor {
				p.mu.Lock()
				tipo = p.mids[t.Mid()]
				p.mu.Unlock()
			}
		}
		if tipo == "" {
			return
		}
		s.publicarFaixa(p, pc, tipo, remota)
	})

	s.mu.Lock()
	existentes := make([]*faixa, 0, len(s.faixas))
	for _, f := range s.faixas {
		if f.dono != p {
			existentes = append(existentes, f)
		}
	}
	s.mu.Unlock()
	for _, f := range existentes {
		s.assinarFaixa(p, f)
	}
	p.negociar()
	return nil
}

// pararMidia fecha a conexão com o servidor (a pessoa passa a falar direto com os outros).
func (p *participante) pararMidia() {
	p.mu.Lock()
	pc := p.pc
	p.pc = nil
	p.saidas = map[string]*webrtc.RTPSender{}
	p.negociando, p.pendente = false, false
	p.mu.Unlock()
	if pc != nil {
		go pc.Close()
	}
}

// sair tira a pessoa da sala e as faixas dela das conexões dos outros.
func (s *Sala) sair(p *participante) {
	s.ordem.Lock()
	defer s.ordem.Unlock()

	s.mu.Lock()
	if s.participantes[p.estado.UsuarioID] != p {
		s.mu.Unlock()
		p.pararMidia()
		return
	}
	delete(s.participantes, p.estado.UsuarioID)
	var minhas []*faixa
	for k, f := range s.faixas {
		if f.dono == p {
			minhas = append(minhas, f)
			delete(s.faixas, k)
		}
	}
	outros := make([]*participante, 0, len(s.participantes))
	for _, o := range s.participantes {
		outros = append(outros, o)
	}
	total := len(s.participantes)
	modo := s.modo
	s.mu.Unlock()

	// Avisa todo mundo antes de fechar a conexão: fechar uma conexão WebRTC que nunca
	// conectou (ex.: rede sem UDP) pode demorar, e a pessoa não pode ficar "presa" na sala.
	max := s.sfu.o.MaxDireto
	if modo == modoSFU && max > 0 && total > 0 && total < max {
		// voltou a caber no modo direto (com folga de uma pessoa, para não ficar trocando)
		s.trocarModo(modoDireto)
	}
	s.sfu.avisar(context.Background(), s)
	p.pararMidia()
	if modo == modoSFU {
		for _, o := range outros {
			mudou := false
			for _, f := range minhas {
				o.mu.Lock()
				envio := o.saidas[f.chave()]
				delete(o.saidas, f.chave())
				pc := o.pc
				o.mu.Unlock()
				if envio != nil && pc != nil {
					_ = pc.RemoveTrack(envio)
					mudou = true
				}
			}
			if mudou {
				o.negociar()
			}
		}
	}
	if total == 0 {
		s.sfu.esvaziou(s)
	}
}

// publicarFaixa registra uma faixa nova de alguém e passa a repassá-la aos outros.
func (s *Sala) publicarFaixa(p *participante, pc *webrtc.PeerConnection, tipo string, remota *webrtc.TrackRemote) {
	local, err := webrtc.NewTrackLocalStaticRTP(remota.Codec().RTPCodecCapability, tipo, "u"+strconv.FormatInt(p.estado.UsuarioID, 10))
	if err != nil {
		slog.Error("voz: faixa local", "erro", err)
		return
	}
	f := &faixa{dono: p, pc: pc, tipo: tipo, local: local, remota: remota}
	s.mu.Lock()
	if atual, _ := p.conexao(); s.participantes[p.estado.UsuarioID] != p || s.modo != modoSFU || atual != pc {
		s.mu.Unlock()
		return
	}
	s.faixas[f.chave()] = f
	outros := make([]*participante, 0, len(s.participantes))
	for _, o := range s.participantes {
		if o != p {
			outros = append(outros, o)
		}
	}
	s.mu.Unlock()

	for _, o := range outros {
		s.assinarFaixa(o, f)
		o.negociar()
	}

	// Repassa os pacotes. Áudio só de quem pode falar e não foi silenciado; vídeo só de
	// quem pode usar câmera/tela. Nada é decodificado.
	go func() {
		ehAudio := tipo == "audio" // microfone; o som da tela segue a permissão de vídeo
		for {
			pacote, _, err := remota.ReadRTP()
			if err != nil {
				return
			}
			p.mu.Lock()
			liberado := (ehAudio && p.perm.Tem(permissoes.Falar) && !p.estado.Silenciado) ||
				(!ehAudio && p.perm.Tem(permissoes.Video))
			p.mu.Unlock()
			if !liberado {
				continue
			}
			_ = local.WriteRTP(pacote) // erro aqui é de quem recebe (ex.: saiu); segue para os outros
		}
	}()
	// Vídeo: um quadro completo logo no começo, para quem já está na sala ver na hora.
	// Depois, só quando alguém pedir (entrou no meio ou perdeu pacotes): quadros completos
	// são grandes, e pedir de tempos em tempos deixava a transmissão de tela engasgando.
	if tipo == "camera" || tipo == "tela" {
		s.pedirQuadro(f)
	}
}

// pedirQuadro pede ao navegador de quem manda o vídeo um quadro completo (keyframe).
func (s *Sala) pedirQuadro(f *faixa) {
	_ = f.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(f.remota.SSRC())}})
}

// assinarFaixa coloca a faixa f na conexão de o (sem negociar ainda).
func (s *Sala) assinarFaixa(o *participante, f *faixa) {
	// Sempre um transceptor novo, só de envio (o AddTrack poderia reaproveitar um dos três
	// de recebimento e bagunçar o mapa de mids).
	pc, _ := o.conexao()
	if pc == nil {
		return
	}
	t, err := pc.AddTransceiverFromTrack(f.local, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		slog.Warn("voz: adicionar faixa", "erro", err)
		return
	}
	envio := t.Sender()
	o.mu.Lock()
	if o.pc != pc {
		o.mu.Unlock()
		return
	}
	o.saidas[f.chave()] = envio
	o.mu.Unlock()
	// Lê o RTCP de quem recebe: pedidos de quadro completo vão para quem manda.
	go func() {
		for {
			pacotes, _, err := envio.ReadRTCP()
			if err != nil {
				return
			}
			for _, pk := range pacotes {
				switch pk.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					s.pedirQuadro(f)
				}
			}
		}
	}()
	if f.tipo == "camera" || f.tipo == "tela" {
		s.pedirQuadro(f)
	}
}

// negociar manda uma oferta nova (uma de cada vez; se já houver uma em andamento, outra
// sai assim que a resposta chegar).
func (p *participante) negociar() {
	p.mu.Lock()
	pc, g := p.pc, p.geracao
	if pc == nil {
		p.mu.Unlock()
		return
	}
	if p.negociando {
		p.pendente = true
		p.mu.Unlock()
		return
	}
	p.negociando = true
	p.mu.Unlock()

	oferta, err := pc.CreateOffer(nil)
	if err == nil {
		err = pc.SetLocalDescription(oferta)
	}
	if err != nil {
		if atual, _ := p.conexao(); atual != pc {
			return // a conexão foi trocada no meio (troca de modo)
		}
		slog.Warn("voz: oferta", "erro", err)
		p.fechar()
		return
	}
	// Mapeia os mids dos transceptores de recebimento (os quatro primeiros) para os tipos.
	mids := map[string]string{}
	p.mu.Lock()
	for i, t := range pc.GetTransceivers() {
		if i < len(tiposFaixa) && t.Mid() != "" {
			p.mids[t.Mid()] = tiposFaixa[i]
			mids[t.Mid()] = tiposFaixa[i]
		}
	}
	p.mu.Unlock()
	p.mandar("VOZ_OFERTA", map[string]any{"sdp": pc.LocalDescription().SDP, "mids": mids, "g": g})
}

func (p *participante) resposta(g int, sdp string) error {
	pc, atual := p.conexao()
	if pc == nil || g != atual {
		return nil // resposta de uma conexão que já foi trocada
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		return err
	}
	p.mu.Lock()
	p.negociando = false
	outra := p.pendente
	p.pendente = false
	p.mu.Unlock()
	if outra {
		p.negociar()
	}
	return nil
}

// atender lê o que o navegador manda pela conexão de voz.
func (p *participante) atender(ctx context.Context) error {
	for {
		tipo, bruto, err := p.ws.Read(ctx)
		if err != nil {
			return err
		}
		if tipo != websocket.MessageText {
			continue
		}
		var m struct {
			Tipo  string          `json:"t"`
			Dados json.RawMessage `json:"d"`
		}
		if json.Unmarshal(bruto, &m) != nil {
			continue
		}
		switch m.Tipo {
		case "VOZ_RESPOSTA":
			var d struct {
				SDP     string `json:"sdp"`
				Geracao int    `json:"g"`
			}
			if json.Unmarshal(m.Dados, &d) == nil {
				if err := p.resposta(d.Geracao, d.SDP); err != nil {
					slog.Warn("voz: resposta", "erro", err)
					return err
				}
			}
		case "VOZ_ICE":
			var d struct {
				Candidato webrtc.ICECandidateInit `json:"candidato"`
				Geracao   int                     `json:"g"`
			}
			if json.Unmarshal(m.Dados, &d) == nil {
				if pc, g := p.conexao(); pc != nil && g == d.Geracao {
					_ = pc.AddICECandidate(d.Candidato)
				}
			}
		case "VOZ_P2P":
			// Modo direto: repassa oferta/resposta/candidatos para outra pessoa da sala.
			var d struct {
				Para  int64           `json:"para"`
				Sinal json.RawMessage `json:"sinal"`
			}
			if json.Unmarshal(m.Dados, &d) != nil || len(d.Sinal) == 0 {
				continue
			}
			s := p.sala
			s.mu.Lock()
			alvo := s.participantes[d.Para]
			direto := s.modo == modoDireto
			s.mu.Unlock()
			if alvo != nil && alvo != p && direto {
				alvo.mandar("VOZ_P2P", map[string]any{"de": p.estado.UsuarioID, "sinal": d.Sinal})
			}
		case "VOZ_ESTADO":
			var d struct {
				Mudo, Surdo, Camera, Tela bool
			}
			if json.Unmarshal(m.Dados, &d) != nil {
				continue
			}
			p.mu.Lock()
			p.estado.Mudo, p.estado.Surdo = d.Mudo || !p.perm.Tem(permissoes.Falar), d.Surdo
			p.estado.Camera = d.Camera && p.perm.Tem(permissoes.Video)
			p.estado.Tela = d.Tela && p.perm.Tem(permissoes.Video)
			p.mu.Unlock()
			p.sala.sfu.avisar(ctx, p.sala)
		case "VOZ_MODERAR":
			var d struct {
				UsuarioID int64  `json:"usuarioId"`
				Acao      string `json:"acao"`
			}
			if json.Unmarshal(m.Dados, &d) != nil {
				continue
			}
			if err := p.sala.sfu.moderar(ctx, p, d.UsuarioID, d.Acao); err != nil {
				p.mandar("VOZ_ERRO", map[string]string{"mensagem": err.Error()})
			}
		}
	}
}
