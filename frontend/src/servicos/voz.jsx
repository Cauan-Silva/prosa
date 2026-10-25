// Voz do Prosa, em dois modos que o servidor escolhe pelo tamanho da sala:
//
// - "direto" (salas pequenas): cada navegador abre uma conexão WebRTC com cada um dos
//   outros e a mídia vai direto, sem passar pelo servidor (que só repassa a negociação).
//   Quem tem o id menor liga; cada conexão já nasce com as quatro faixas (microfone,
//   câmera, tela e som da tela), então ligar a câmera é só trocar a faixa, sem renegociar.
// - "sfu" (salas grandes): uma conexão só com o servidor de mídia. O servidor manda as
//   ofertas e o navegador responde; as quatro primeiras faixas são as de envio e o que
//   chega dos outros vem em faixas novas, identificadas pelo id do "stream" (u<id>).
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, lerToken } from './api.js'
import { salaDeVoz, tocar } from './sons.js'
import { aoMudar, mudar, preferencias, usePreferencias } from './preferencias.js'
import { Processador, abrirMicrofone } from './microfone.js'
import { aoPttGlobal, aoSairDaVozDesktop, definirPttGlobal, noDesktop } from './desktop.js'

const Contexto = createContext(null)
const TIPOS = ['audio', 'camera', 'tela', 'telaAudio']
const LIMIAR_FALA = 0.015 // ~ -36 dBFS
const SEGURA_FALA_MS = 280

/** Prefere H.264 no vídeo: no Windows costuma ser codificado pela placa de vídeo (bem menos processador). */
function preferirH264(transceptor) {
  try {
    const caps = RTCRtpReceiver.getCapabilities?.('video')
    if (!caps || !transceptor?.setCodecPreferences) return
    const ehH264 = (c) => c.mimeType.toLowerCase() === 'video/h264'
    const h264 = caps.codecs.filter(ehH264)
    if (h264.length) transceptor.setCodecPreferences([...h264, ...caps.codecs.filter((c) => !ehH264(c))])
  } catch {
    // navegador sem suporte: fica com o codec padrão
  }
}

// Codec da tela no modo direto. AV1 e VP9 entregam a mesma imagem com bem menos banda que
// o H.264 (o AV1 chega a ~40% a menos, e ainda mais em texto e código): como no modo direto
// cada pessoa recebe uma cópia, isso é o que mais alivia o upload de quem transmite.
// Só compensa quando a placa de vídeo codifica; com "usar mais o processador" ligado, o
// processador pode fazer esse trabalho mais pesado no lugar dela.
// Todos os codecs continuam negociados; quem transmite só escolhe qual usar para enviar.
export async function escolherCodecTela(potente) {
  const mc = navigator.mediaCapabilities
  if (!mc?.encodingInfo || !window.RTCRtpSender?.getCapabilities) return null
  const tem = new Set((RTCRtpSender.getCapabilities('video')?.codecs || []).map((c) => c.mimeType.toLowerCase()))
  const testar = async (mime) => {
    if (!tem.has(mime.toLowerCase())) return null
    try {
      return await mc.encodingInfo({ type: 'webrtc', video: { contentType: mime, width: 1920, height: 1080, bitrate: 4_000_000, framerate: 30 } })
    } catch {
      return null
    }
  }
  const [av1, vp9, h264] = await Promise.all(['video/AV1', 'video/VP9', 'video/H264'].map(testar))
  const leve = h264?.powerEfficient ? { mime: 'video/H264', gpu: true } : null
  if (av1?.powerEfficient) return { mime: 'video/AV1', gpu: true, reserva: leve }
  if (vp9?.powerEfficient) return { mime: 'video/VP9', gpu: true, reserva: leve }
  if (potente && av1?.supported && av1.smooth) return { mime: 'video/AV1', gpu: false, reserva: leve }
  if (potente && vp9?.supported) return { mime: 'video/VP9', gpu: false, reserva: leve }
  return leve
}

const NOMES_CODEC = { 'video/av1': 'AV1', 'video/vp9': 'VP9', 'video/vp8': 'VP8', 'video/h264': 'H.264' }

const tipoDeMidia = (tipo) => (tipo === 'audio' || tipo === 'telaAudio' ? 'audio' : 'video')

// Modos da tela compartilhada: quadros por segundo capturados e enviados, e quanta banda
// cada cópia pode usar (no modo direto o total é dividido entre quem assiste).
// "fps60" baixa a resolução antes de perder quadros: bom para jogos e vídeos.
const MODOS_TELA = {
  // automático: começa em 1080p 30 fps e sobe ou desce sozinho (veja NIVEIS_AUTO)
  // (fluidez primeiro: numa oscilação a resolução cai um pouco, mas os quadros continuam)
  auto: { captura: 30, maxFramerate: 30, maxBitrate: 4_000_000, total: 16_000_000, minimo: 800_000, preferencia: 'maintain-framerate', dica: 'motion' },
  nitido: { captura: 15, maxFramerate: 10, maxBitrate: 1_200_000, total: 3_000_000, minimo: 500_000, preferencia: 'maintain-resolution', dica: 'detail' },
  fluido: { captura: 30, maxFramerate: 30, maxBitrate: 2_000_000, total: 4_000_000, minimo: 700_000, preferencia: 'maintain-framerate', dica: 'motion' },
  fps60: { captura: 60, maxFramerate: 60, maxBitrate: 4_500_000, total: 8_000_000, minimo: 1_500_000, preferencia: 'maintain-framerate', dica: 'motion' },
}
const modoDaTela = (modo) => MODOS_TELA[modo] || MODOS_TELA.auto

// Degraus do modo automático, do melhor para o mais leve. A resolução é reduzida a partir
// do que é capturado (até 1080p): escala 1.5 = 720p, 2 = 540p.
const NIVEIS_AUTO = [
  { fps: 60, escala: 1, bitrate: 6_000_000 },
  { fps: 30, escala: 1, bitrate: 4_000_000 },
  { fps: 30, escala: 1.5, bitrate: 2_500_000 },
  { fps: 24, escala: 2, bitrate: 1_200_000 },
]
const NIVEL_AUTO_INICIAL = 1

// Quem assiste segura este tanto de vídeo antes de mostrar (jitter buffer): os quadros
// saem num ritmo constante mesmo quando os pacotes chegam aos trancos. Custa só um
// atraso pequeno, que numa tela compartilhada quase não se nota.
const ATRASO_TELA_MS = 150

function lerModoTela() {
  try {
    // quem já usava o Prosa passa para o automático uma vez; depois vale o que escolher
    if (!localStorage.getItem('prosa.modoTelaAuto')) {
      localStorage.setItem('prosa.modoTelaAuto', '1')
      localStorage.setItem('prosa.modoTela', 'auto')
    }
    return localStorage.getItem('prosa.modoTela') || 'auto'
  } catch {
    return 'auto'
  }
}

const inicial = {
  canalId: null,
  servidorId: null,
  estado: 'fora', // fora | conectando | conectada | erro
  erro: '',
  mudo: false,
  surdo: false,
  camera: false,
  tela: false,
  semMicrofone: false,
  telaComSom: false,
  modoTela: lerModoTela(),
}

function enderecoVoz(base, canalId) {
  const origem = base || location.origin
  const u = new URL(`/api/voz/${canalId}`, origem)
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:'
  u.searchParams.set('t', lerToken() || '')
  return u.toString()
}

/** Explica em português por que o microfone não abriu (o navegador só dá o nome do erro). */
function motivoMicrofone(e) {
  const desktop = !!window.__TAURI__
  switch (e?.name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return desktop
        ? 'O microfone foi bloqueado. No Windows, abra Configurações → Privacidade e segurança → Microfone e ligue “Permitir que aplicativos da área de trabalho acessem o microfone”. Se você recusou o pedido do Prosa, veja em Configurações → App como pedir de novo.'
        : 'O microfone foi bloqueado. Clique no cadeado ao lado do endereço do site, permita o microfone e entre na sala de novo.'
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'Nenhum microfone encontrado. Confira se ele está conectado e escolha em Configurações → Voz e vídeo.'
    case 'NotReadableError':
    case 'AbortError':
      return 'O microfone está sendo usado por outro programa (ou o Windows não deixou abrir). Feche o outro programa e entre na sala de novo.'
    default:
      return `Não foi possível abrir o microfone${e?.name ? ` (${e.name})` : ''}.`
  }
}

class Sessao {
  constructor(aoMudar) {
    this.aoMudar = aoMudar
    this.faixas = { audio: null, camera: null, tela: null, telaAudio: null }
    this.locais = { camera: null, tela: null } // prévias do que eu mando
    this.remotos = {} // usuarioId -> { audio, camera, tela } (MediaStream)
    this.mids = {}
    this.geracao = 0 // conexão com o servidor (modo sfu)
    this.icePendentes = []
    this.modo = null
    this.participantes = []
    this.pares = new Map() // usuarioId -> conexão direta (modo direto)
    this.iceAntecipado = {}
    this.fila = Promise.resolve()
    this.niveis = {}
    this.fechada = false
    this.modoTela = 'auto'
    this.auto = null // estado do modo automático da tela
    this.infoTela = '' // o que está sendo enviado agora (ex.: "1080p · 60 fps")
    this.codecTela = null // { mime, gpu, reserva } escolhido ao começar a compartilhar (modo direto)
    this.recepcao = {} // usuarioId -> receptor da tela dessa pessoa (para medir o que chega)
    this.infoRecebida = {} // usuarioId -> o que está chegando (ex.: "1080p · 30 fps · perda 2%")
    this.timerRecepcao = null
  }

  async entrar(servidorId, canalId, desejado) {
    this.servidorId = servidorId
    this.canalId = canalId
    this.desejado = desejado
    const info = await api.vozInfo(canalId)
    this.ice = info.ice
    try {
      await this.abrirMicrofone()
    } catch (e) {
      this.semMicrofone = true // entra só para ouvir
      this.motivoSemMicrofone = motivoMicrofone(e)
      console.warn('microfone:', e?.name, e?.message)
    }
    await this.conectar(info.endereco, 0)
  }

  // Abre (ou reabre, ao trocar de microfone ou de opção) o microfone com o processamento.
  async abrirMicrofone() {
    const voz = preferencias().voz
    const novo = new Processador(await abrirMicrofone(voz), voz)
    const antigo = this.proc
    this.proc = novo
    novo.track.enabled = !this.desejado.mudo && !this.desejado.surdo
    this.niveis.eu = () => novo.nivelSaida() // depois do portão e do pressionar para falar
    await this.trocarFaixa('audio', novo.track)
    antigo?.destruir()
    this.semMicrofone = false
  }

  async aplicarVoz(antes, depois) {
    const reabrir = ['microfone', 'supressaoRuido', 'supressaoIA', 'cancelamentoEco', 'ganhoAutomatico'].some((k) => antes[k] !== depois[k])
    if (reabrir && (this.proc || this.semMicrofone)) {
      try {
        await this.abrirMicrofone()
      } catch {
        // continua com o microfone anterior
      }
    } else this.proc?.setOpcoes(depois)
    if (antes.camera !== depois.camera && this.faixas.camera) {
      try {
        const m = await navigator.mediaDevices.getUserMedia({ video: restricoesDeCamera(depois) })
        await this.trocarFaixa('camera', m.getVideoTracks()[0])
      } catch {
        // câmera nova indisponível
      }
    }
  }

  conectar(endereco, tentativa) {
    return new Promise((resolver, rejeitar) => {
      const ws = new WebSocket(enderecoVoz(endereco, this.canalId))
      this.ws = ws
      let pronta = false
      ws.onmessage = (m) => {
        const { t, d } = JSON.parse(m.data)
        if (t === 'VOZ_PRONTO') {
          pronta = true
          this.eu = d.usuarioId
          this.ice = d.ice || this.ice
          this.enviarEstado()
          resolver()
        } else if (t === 'VOZ_REDIRECIONAR' && tentativa < 2) {
          ws.onclose = null
          this.conectar(d.endereco, tentativa + 1).then(resolver, rejeitar)
        } else if (t === 'VOZ_OFERTA') {
          this.fila = this.fila.then(() => this.responder(d)).catch((e) => console.warn('voz: oferta', e))
        } else if (t === 'VOZ_ICE') {
          if (d.g === this.geracao) this.fila = this.fila.then(() => this.pc?.addIceCandidate(d.candidato)).catch(() => {})
          else if (d.g > this.geracao) this.icePendentes.push(d) // chegou antes da oferta
        } else if (t === 'VOZ_SALA') {
          this.atualizarSala(d)
        } else if (t === 'VOZ_P2P') {
          this.sinal(d.de, d.sinal)
        } else if (t === 'VOZ_ERRO') {
          this.erro = d.mensagem
          this.aoMudar()
        }
      }
      ws.onclose = () => {
        if (!pronta) rejeitar(new Error(this.erro || 'Não foi possível entrar na sala de voz.'))
        else if (!this.fechada) this.aoMudar('caiu')
      }
    })
  }

  // ---- modo sfu: uma conexão com o servidor ----

  criarConexao(g) {
    this.fecharServidor()
    const pc = new RTCPeerConnection({ iceServers: this.ice })
    this.pc = pc
    this.geracao = g
    pc.onicecandidate = (e) => e.candidate && this.mandar('VOZ_ICE', { candidato: e.candidate.toJSON(), g })
    pc.onconnectionstatechange = () => {
      if (this.pc !== pc) return
      if (pc.connectionState === 'failed') {
        this.erro = 'A conexão de mídia falhou. Sua rede pode estar bloqueando UDP (o servidor precisa de TURN).'
        this.aoMudar('caiu')
      }
      this.aoMudar()
    }
    pc.ontrack = (e) => {
      const stream = e.streams[0]
      const id = Number(stream?.id?.replace(/^u/, ''))
      if (!id) return
      const tipo = ['audio', 'camera', 'tela', 'telaAudio'].includes(e.track.id) ? e.track.id : e.track.kind === 'audio' ? 'audio' : 'camera'
      const atual = (this.remotos[id] ||= {})
      atual[tipo] = new MediaStream([e.track])
      if (tipo === 'audio') this.medir(id, atual.audio)
      else if (tipo === 'tela' || tipo === 'telaAudio') this.suavizarTela(id, tipo, e.receiver)
      e.track.onended = () => {
        delete atual[tipo]
        this.aoMudar()
      }
      this.aoMudar()
    }
  }

  fecharServidor() {
    if (!this.pc) return
    this.pc.onconnectionstatechange = null
    this.pc.close()
    this.pc = null
    this.mids = {}
  }

  async responder({ sdp, mids, g }) {
    if (g !== this.geracao || !this.pc) this.criarConexao(g)
    const pc = this.pc
    this.mids = mids
    await pc.setRemoteDescription({ type: 'offer', sdp })
    const pendentes = this.icePendentes.filter((c) => c.g === g)
    this.icePendentes = this.icePendentes.filter((c) => c.g > g)
    for (const c of pendentes) pc.addIceCandidate(c.candidato).catch(() => {})
    for (const t of pc.getTransceivers()) {
      const tipo = mids[t.mid]
      if (!tipo) continue
      if (t.direction !== 'sendonly') t.direction = 'sendonly'
      if (tipo === 'tela') preferirH264(t)
      const faixa = this.faixas[tipo]
      if (t.sender.track !== faixa) await t.sender.replaceTrack(faixa)
    }
    const resposta = await pc.createAnswer()
    await pc.setLocalDescription(resposta)
    this.mandar('VOZ_RESPOSTA', { sdp: pc.localDescription.sdp, g })
    await this.ajustarQualidade()
  }

  // ---- quem está na sala e em que modo ----

  atualizarSala({ modo, participantes }) {
    this.participantes = participantes || []
    if (modo !== this.modo) {
      this.modo = modo
      // troca de modo: tudo que chegava pelo caminho antigo para
      if (modo === 'direto') this.fecharServidor()
      for (const id of [...this.pares.keys()]) this.fecharPar(id)
      this.remotos = {}
      this.recepcao = {}
      this.infoRecebida = {}
      this.niveis = this.niveis.eu ? { eu: this.niveis.eu } : {}
    }
    if (modo === 'direto') this.sincronizarPares()
    this.aoMudar()
  }

  // No modo direto quem recebe é que corta a mídia de quem foi silenciado ou não tem permissão.
  bloqueado(id, tipo) {
    const p = this.participantes.find((x) => x.usuarioId === Number(id))
    if (!p) return false
    if (tipo === 'audio') return p.silenciado || p.semVoz
    return p.semVideo || !p.tela // som da tela só enquanto a pessoa compartilha
  }

  // ---- modo direto: uma conexão com cada pessoa ----

  sincronizarPares() {
    const ids = new Set(this.participantes.map((p) => p.usuarioId).filter((id) => id !== this.eu))
    const antes = this.pares.size
    for (const id of [...this.pares.keys()]) if (!ids.has(id)) this.fecharPar(id)
    if (this.pares.size < antes) this.ajustarQualidade() // menos cópias: cada uma pode ter mais qualidade
    // quem tem o id menor liga; o outro espera a oferta
    for (const id of ids) if (!this.pares.has(id) && this.eu < id) this.ligarPara(id, 1, 0)
  }

  novoPar(id, n, inicia) {
    this.fecharPar(id)
    const pc = new RTCPeerConnection({ iceServers: this.ice })
    const par = { id, n, pc, inicia, fila: Promise.resolve() }
    pc.onicecandidate = (e) => e.candidate && this.mandarPar(id, { tipo: 'ice', n, candidato: e.candidate.toJSON() })
    pc.ontrack = (e) => {
      const tipo = TIPOS[pc.getTransceivers().indexOf(e.transceiver)]
      if (!tipo || this.pares.get(id) !== par) return
      const atual = (this.remotos[id] ||= {})
      atual[tipo] = new MediaStream([e.track])
      if (tipo === 'audio') this.medir(id, atual.audio)
      else if (tipo === 'tela' || tipo === 'telaAudio') this.suavizarTela(id, tipo, e.receiver)
      this.aoMudar()
    }
    pc.onconnectionstatechange = () => {
      if (this.pares.get(id) !== par) return
      // caiu (troca de rede, por exemplo): quem ligou tenta de novo algumas vezes
      if (pc.connectionState === 'failed' && inicia && par.tentativas < 5) {
        setTimeout(() => this.pares.get(id) === par && !this.fechada && this.ligarPara(id, n + 1, par.tentativas + 1), 1000 * (par.tentativas + 1))
      }
      this.aoMudar()
    }
    this.pares.set(id, par)
    return par
  }

  async ligarPara(id, n, tentativas) {
    const par = this.novoPar(id, n, true)
    par.tentativas = tentativas
    const pc = par.pc
    try {
      for (const tipo of TIPOS) {
        const t = pc.addTransceiver(this.faixas[tipo] || tipoDeMidia(tipo), { direction: 'sendrecv' })
        if (tipo === 'tela') preferirH264(t)
      }
      await pc.setLocalDescription(await pc.createOffer())
      if (this.pares.get(id) !== par) return
      this.mandarPar(id, { tipo: 'oferta', n, sdp: pc.localDescription.sdp })
      await this.ajustarQualidade()
    } catch (e) {
      console.warn('voz: ligar direto', e)
    }
  }

  sinal(de, s) {
    if (!s || this.modo !== 'direto') return
    if (s.tipo === 'oferta') {
      const par = this.novoPar(de, s.n, false)
      par.tentativas = 0
      const pc = par.pc
      par.fila = par.fila
        .then(async () => {
          await pc.setRemoteDescription({ type: 'offer', sdp: s.sdp })
          const ts = pc.getTransceivers()
          for (let i = 0; i < ts.length && i < TIPOS.length; i++) {
            ts[i].direction = 'sendrecv'
            if (TIPOS[i] === 'tela') preferirH264(ts[i])
            await ts[i].sender.replaceTrack(this.faixas[TIPOS[i]] || null)
          }
          await pc.setLocalDescription(await pc.createAnswer())
          if (this.pares.get(de) !== par) return
          this.mandarPar(de, { tipo: 'resposta', n: s.n, sdp: pc.localDescription.sdp })
          for (const c of (this.iceAntecipado[de] || []).filter((c) => c.n === s.n)) pc.addIceCandidate(c.candidato).catch(() => {})
          delete this.iceAntecipado[de]
          await this.ajustarQualidade()
        })
        .catch((e) => console.warn('voz: oferta direta', e))
      return
    }
    const par = this.pares.get(de)
    if (!par || par.n !== s.n) {
      // candidato que chegou antes da oferta: guarda para depois
      if (s.tipo === 'ice' && (!par || s.n > par.n)) (this.iceAntecipado[de] ||= []).push(s)
      return
    }
    par.fila = par.fila
      .then(async () => {
        if (s.tipo !== 'resposta') return par.pc.addIceCandidate(s.candidato)
        await par.pc.setRemoteDescription({ type: 'answer', sdp: s.sdp })
        await this.ajustarQualidade() // agora os codecs estão negociados
      })
      .catch(() => {})
  }

  mandarPar(para, sinal) {
    this.mandar('VOZ_P2P', { para, sinal })
  }

  fecharPar(id) {
    const par = this.pares.get(id)
    if (!par) return
    this.pares.delete(id)
    par.pc.onconnectionstatechange = null
    par.pc.close()
    delete this.remotos[id]
    delete this.niveis[id]
    delete this.recepcao[id]
    delete this.infoRecebida[id]
  }

  // ---- quem assiste uma tela ----

  suavizarTela(id, tipo, receptor) {
    try {
      if ('jitterBufferTarget' in receptor) receptor.jitterBufferTarget = ATRASO_TELA_MS
      else if ('playoutDelayHint' in receptor) receptor.playoutDelayHint = ATRASO_TELA_MS / 1000
    } catch {
      // navegador sem esse ajuste: mostra assim que chega
    }
    if (tipo !== 'tela') return
    this.recepcao[id] = { receptor, antes: null }
    this.timerRecepcao ||= setInterval(() => this.medirRecepcao(), 2000)
  }

  // Mede o que chega de cada tela: resolução, quadros por segundo, pacotes perdidos e
  // travadas. Ajuda a ver se o engasgo vem da internet (perda) ou de quem manda (fps baixo).
  async medirRecepcao() {
    for (const [id, r] of Object.entries(this.recepcao)) {
      if (r.receptor.track?.readyState !== 'live') {
        delete this.recepcao[id]
        delete this.infoRecebida[id]
        continue
      }
      const st = await r.receptor.getStats().catch(() => null)
      if (!st || this.recepcao[id] !== r) continue
      let s = null
      st.forEach((x) => {
        if (x.type === 'inbound-rtp' && x.kind === 'video') s = x
      })
      if (!s) continue
      const a = r.antes
      r.antes = s
      const partes = []
      if (s.frameHeight) partes.push(`${s.frameHeight}p`)
      if (s.framesPerSecond != null) partes.push(`${Math.round(s.framesPerSecond)} fps`)
      if (a) {
        const recebidos = (s.packetsReceived || 0) - (a.packetsReceived || 0)
        const perdidos = Math.max(0, (s.packetsLost || 0) - (a.packetsLost || 0))
        const pct = recebidos + perdidos > 0 ? (perdidos / (recebidos + perdidos)) * 100 : 0
        if (pct >= 0.5) partes.push(`perda ${pct < 10 ? pct.toFixed(1) : Math.round(pct)}%`)
        const travadas = (s.freezeCount || 0) - (a.freezeCount || 0)
        if (travadas > 0) partes.push(travadas === 1 ? '1 travada' : `${travadas} travadas`)
      }
      if (s.powerEfficientDecoder === false) partes.push('decodificando no processador')
      this.infoRecebida[id] = partes.join(' · ')
    }
    if (!Object.keys(this.recepcao).length) {
      clearInterval(this.timerRecepcao)
      this.timerRecepcao = null
    }
    if (!this.fechada) this.aoMudar()
  }

  // Todos os "envios" de um tipo de faixa: o da conexão com o servidor e o de cada conexão direta.
  envios(tipo) {
    const lista = []
    const t = this.transceptor(tipo)
    if (t) lista.push(t.sender)
    const i = TIPOS.indexOf(tipo)
    for (const par of this.pares.values()) {
      const tp = par.pc.getTransceivers()[i]
      if (tp) lista.push(tp.sender)
    }
    return lista
  }

  // Limites de envio: a tela pode gastar bem mais banda que a câmera. No modo "fluido",
  // o navegador baixa a resolução antes de baixar os quadros por segundo; no "nítido",
  // faz o contrário (melhor para texto e código parado).
  // No modo direto cada pessoa recebe uma cópia, então o limite de cada uma diminui
  // conforme a sala cresce, para não estourar o upload de quem manda.
  async ajustarQualidade() {
    const copias = Math.max(1, this.pares.size)
    const dividir = (maximo, total, minimo) => Math.max(minimo, Math.min(maximo, Math.round(total / copias)))
    const regras = {
      camera: { maxBitrate: dividir(600_000, 1_800_000, 250_000), maxFramerate: 24, preferencia: 'balanced' },
      telaAudio: { maxBitrate: 96_000 }, // som da tela com qualidade de música
      tela: (() => {
        const m = modoDaTela(this.modoTela)
        if (this.modoTela === 'auto' && this.auto) {
          const n = NIVEIS_AUTO[this.auto.nivel]
          return { maxBitrate: dividir(n.bitrate, m.total, m.minimo), maxFramerate: n.fps, preferencia: m.preferencia, escala: n.escala }
        }
        return { maxBitrate: dividir(m.maxBitrate, m.total, m.minimo), maxFramerate: m.maxFramerate, preferencia: m.preferencia, escala: 1 }
      })(),
    }
    for (const [tipo, r] of Object.entries(regras)) {
      for (const envio of this.envios(tipo)) {
        if (!envio.track) continue
        try {
          const p = envio.getParameters()
          if (!p.encodings?.length) p.encodings = [{}]
          p.encodings[0].maxBitrate = r.maxBitrate
          if (r.maxFramerate) p.encodings[0].maxFramerate = r.maxFramerate
          if (r.escala) p.encodings[0].scaleResolutionDownBy = r.escala
          if (r.preferencia) p.degradationPreference = r.preferencia
          await envio.setParameters(p)
        } catch {
          // navegador sem suporte a algum ajuste (ou conexão ainda negociando): segue com o padrão
        }
        // codec da tela: só nas conexões diretas (o servidor repassa o codec com que a faixa começou)
        if (tipo === 'tela' && envio !== this.transceptor('tela')?.sender) await this.aplicarCodec(envio)
      }
    }
  }

  async aplicarCodec(envio) {
    try {
      const p = envio.getParameters()
      if (!p.encodings?.length || !p.codecs?.length) return // ainda negociando
      const quero = this.codecTela?.mime?.toLowerCase()
      const atual = p.encodings[0].codec?.mimeType?.toLowerCase()
      if (quero === atual) return
      const c = quero && p.codecs.find((x) => x.mimeType.toLowerCase() === quero)
      if (quero && !c) return // a outra pessoa não decodifica esse: fica com o negociado
      if (c) p.encodings[0].codec = { mimeType: c.mimeType, clockRate: c.clockRate, ...(c.sdpFmtpLine ? { sdpFmtpLine: c.sdpFmtpLine } : {}) }
      else delete p.encodings[0].codec
      await envio.setParameters(p)
    } catch {
      // navegador sem escolha de codec por envio: fica com o negociado (H.264)
    }
  }

  async mudarModoTela(modo) {
    this.modoTela = modo
    const faixa = this.faixas.tela
    if (modo === 'auto') {
      if (faixa) return this.iniciarAuto()
    } else {
      this.pararAuto()
      if (faixa) this.iniciarAuto(false) // nos outros modos só mede, para mostrar o que sai
    }
    if (faixa) {
      const m = modoDaTela(modo)
      faixa.contentHint = m.dica
      try {
        await faixa.applyConstraints({ frameRate: { ideal: m.captura, max: m.captura } })
      } catch {
        // alguns navegadores não deixam mudar com a tela já compartilhada
      }
    }
    await this.ajustarQualidade()
  }

  transceptor(tipo) {
    return this.pc?.getTransceivers().find((t) => this.mids[t.mid] === tipo)
  }

  // ---- modo automático da tela ----
  // A cada 2 s lê as estatísticas de envio do navegador. Se o processador ou a internet
  // estão segurando o vídeo (qualityLimitationReason), desce um degrau; se ficou 8 s sem
  // nenhum limite, sobe. Tela parada manda poucos quadros, e isso não conta como problema.

  iniciarAuto(adaptar = true) {
    this.pararAuto()
    const agora = performance.now()
    this.auto = { adaptar, nivel: NIVEL_AUTO_INICIAL, amostras: [], bloqueio: {}, inicio: agora, ultimaTroca: agora }
    this.timerAuto = setInterval(() => this.medirTela(), 2000)
    return adaptar ? this.aplicarNivelAuto() : undefined
  }

  pararAuto() {
    clearInterval(this.timerAuto)
    this.timerAuto = null
    this.auto = null
    this.infoTela = ''
  }

  async aplicarNivelAuto() {
    const n = NIVEIS_AUTO[this.auto.nivel]
    const faixa = this.faixas.tela
    if (faixa) {
      faixa.contentHint = 'motion'
      await faixa.applyConstraints({ frameRate: { ideal: n.fps, max: n.fps } }).catch(() => {})
    }
    await this.ajustarQualidade()
  }

  async medirTela() {
    const a = this.auto
    if (!a || !this.faixas.tela || this.fechada) return
    let pior = null
    for (const envio of this.envios('tela')) {
      if (!envio.track) continue
      const st = await envio.getStats().catch(() => null)
      if (!st) continue
      let s = null
      let banda = null // banda que o navegador estima ter para esta conexão
      let codecId = null
      let fonte = null // quadros por segundo que a captura da tela entrega
      st.forEach((r) => {
        if (r.type === 'outbound-rtp' && r.kind === 'video') codecId = r.codecId
        if (r.type === 'media-source' && r.kind === 'video' && r.framesPerSecond != null) fonte = r.framesPerSecond
        if (r.type === 'outbound-rtp' && r.kind === 'video')
          s = { fps: r.framesPerSecond || 0, limite: r.qualityLimitationReason || 'none', altura: r.frameHeight || 0, gpu: r.powerEfficientEncoder }
        if (r.type === 'candidate-pair' && r.availableOutgoingBitrate && (r.nominated || r.state === 'succeeded')) banda = Math.max(banda || 0, r.availableOutgoingBitrate)
      })
      if (!s) continue
      s.banda = banda
      s.fonte = fonte
      s.codec = NOMES_CODEC[st.get(codecId)?.mimeType?.toLowerCase()] || ''
      // o pior envio decide (no modo direto há uma cópia por pessoa)
      if (!pior || (s.limite !== 'none' && pior.limite === 'none') || (s.banda ?? Infinity) < (pior.banda ?? Infinity) || s.fps < pior.fps - 3) pior = s
    }
    if (!pior || this.auto !== a) return
    if (!a.adaptar) {
      this.infoTela = this.textoInfoTela(pior, Math.round(pior.fps))
      return this.aoMudar()
    }
    a.amostras.push(pior)
    if (a.amostras.length > 6) a.amostras.shift()
    const agora = performance.now()
    const ultimas = (n) => a.amostras.slice(-n)
    const todas = (n, f) => ultimas(n).length === n && ultimas(n).every(f)
    // No começo (e logo depois de cada troca) o navegador ainda está medindo a internet e
    // acusa "bandwidth" por alguns segundos: isso não é internet ruim.
    const medindo = agora - a.inicio < 15000 || agora - a.ultimaTroca < 6000
    // O navegador acusa "bandwidth" também enquanto sobe a resolução aos poucos; só é falta
    // de banda de verdade quando a banda estimada não cobre 70% do que o degrau pede.
    const precisa = NIVEIS_AUTO[a.nivel].bitrate
    const faltaBanda = (s) => s.limite === 'bandwidth' && (s.banda == null || s.banda < precisa * 0.7)
    // processador no limite: desce rápido (é o que engasga); banda: só se durar 8 s, porque
    // nas oscilações curtas o próprio codificador já baixa a resolução sozinho
    // A tela captura N quadros mas só saem bem menos: o codificador está descartando quadros
    // porque a banda não cobre essa resolução. O navegador não acusa isso como limite (na
    // tela ele descarta em vez de baixar a resolução), então o fps baixo é o sinal.
    const perdendo = (s) => s.fonte != null && s.fonte >= 12 && s.fps < s.fonte * 0.8 && s.fonte - s.fps >= 4
    const descer = todas(2, (s) => s.limite === 'cpu') || (!medindo && (todas(4, faltaBanda) || todas(3, perdendo)))
    const acima = a.nivel > 0 ? NIVEIS_AUTO[a.nivel - 1].bitrate : 0
    const folga = (s) => s.limite !== 'cpu' && !faltaBanda(s) && !perdendo(s) && (s.banda == null || s.banda >= acima * 0.8)
    let novo = a.nivel
    // processador não deu conta do codec pesado: antes de baixar a qualidade, volta para o
    // H.264 da placa de vídeo (ou o padrão)
    if (todas(2, (s) => s.limite === 'cpu') && this.codecTela && !this.codecTela.gpu) {
      this.codecTela = this.codecTela.reserva
      a.amostras = []
      a.ultimaTroca = agora
      await this.ajustarQualidade()
      return
    }
    if (descer && a.nivel < NIVEIS_AUTO.length - 1 && agora - a.ultimaTroca > 3000) {
      a.bloqueio[a.nivel] = agora + 30000 // não tenta este degrau de novo por 30 s
      novo = a.nivel + 1
    } else if (
      a.nivel > 0 && todas(4, folga) &&
      (a.bloqueio[a.nivel - 1] || 0) < agora && agora - a.ultimaTroca > 8000
    ) {
      novo = a.nivel - 1
    }
    if (novo !== a.nivel) {
      a.nivel = novo
      a.ultimaTroca = agora
      a.amostras = []
      await this.aplicarNivelAuto()
    }
    const fps = Math.round(pior.fps)
    this.infoTela = this.textoInfoTela(pior, `até ${NIVEIS_AUTO[a.nivel].fps} fps${fps ? ` (agora ${fps})` : ''}`)
    this.aoMudar()
  }

  textoInfoTela(s, fps) {
    // quem codifica: a placa de vídeo (leve) ou o processador (pesa, ainda mais com várias cópias)
    const quem = s.gpu === true ? 'na placa de vídeo' : s.gpu === false ? 'no processador' : ''
    const codec = [s.codec, quem].filter(Boolean).join(' ')
    const f = typeof fps === 'number' ? `${fps} fps` : fps
    return [s.altura ? `${s.altura}p` : '', f, codec].filter(Boolean).join(' · ')
  }

  async trocarFaixa(tipo, faixa) {
    const antiga = this.faixas[tipo]
    if (antiga && antiga !== faixa) antiga.stop()
    this.faixas[tipo] = faixa
    if (tipo === 'camera' || tipo === 'tela') this.locais[tipo] = faixa ? new MediaStream([faixa]) : null
    if (tipo === 'tela' && !faixa) this.pararAuto()
    await Promise.all(this.envios(tipo).map((e) => e.replaceTrack(faixa).catch(() => {})))
    if (faixa) await this.ajustarQualidade()
  }

  mandar(t, d) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify({ t, d }))
  }

  enviarEstado() {
    const d = this.desejado
    this.mandar('VOZ_ESTADO', {
      Mudo: d.mudo || d.surdo || this.semMicrofone,
      Surdo: d.surdo,
      Camera: !!this.faixas.camera,
      Tela: !!this.faixas.tela,
    })
  }

  // Mede o volume de cada voz para mostrar quem está falando (RMS de ~40 ms, de 0 a 1).
  medir(chave, stream) {
    try {
      this.audio ||= new AudioContext()
      this.audio.resume().catch(() => {})
      const fonte = this.audio.createMediaStreamSource(stream)
      const analisador = this.audio.createAnalyser()
      analisador.fftSize = 2048
      fonte.connect(analisador)
      const dados = new Float32Array(analisador.fftSize)
      this.niveis[chave] = () => {
        analisador.getFloatTimeDomainData(dados)
        let soma = 0
        for (const v of dados) soma += v * v
        return Math.sqrt(soma / dados.length)
      }
    } catch {
      // sem Web Audio: só não mostra quem fala
    }
  }

  // Quem está falando: acende assim que a voz passa do limite e fica aceso um pouco
  // depois (para não piscar entre uma palavra e outra).
  falando() {
    const agora = performance.now()
    this.ultimaFala ||= {}
    const saida = {}
    for (const [chave, nivel] of Object.entries(this.niveis)) {
      if (nivel() > LIMIAR_FALA) this.ultimaFala[chave] = agora
      if (agora - (this.ultimaFala[chave] || 0) < SEGURA_FALA_MS) saida[chave] = true
    }
    return saida
  }

  sair() {
    this.fechada = true
    this.pararAuto()
    clearInterval(this.timerRecepcao)
    this.ws?.close()
    this.fecharServidor()
    for (const id of [...this.pares.keys()]) this.fecharPar(id)
    for (const f of Object.values(this.faixas)) f?.stop()
    this.proc?.destruir()
    this.audio?.close().catch(() => {})
  }
}

function restricoesDeCamera(voz) {
  return { width: 640, height: 360, frameRate: 24, deviceId: voz.camera ? { exact: voz.camera } : undefined }
}

/** Volume que eu escolhi para alguém (ou para o som da tela de alguém: chave "t" + id). */
export function ganhoDe(voz, chave) {
  const k = String(chave)
  if ((voz.silenciados || []).includes(k)) return 0
  return voz.volumes?.[k] ?? 1
}

/** Muda o volume de alguém só para mim (0 a 2 = 0% a 200%). */
export function mudarVolume(chave, valor) {
  const voz = preferencias().voz
  mudar('voz', { volumes: { ...voz.volumes, [String(chave)]: Math.max(0, Math.min(2, valor)) } })
}

/** Silencia (ou volta a ouvir) alguém só para mim. */
export function alternarSilencioLocal(chave) {
  const k = String(chave)
  const lista = preferencias().voz.silenciados || []
  mudar('voz', { silenciados: lista.includes(k) ? lista.filter((x) => x !== k) : [...lista, k] })
}

export function VozProvider({ children }) {
  const prefs = usePreferencias()
  const [estado, setEstado] = useState(inicial)
  const [, setVersao] = useState(0)
  const [falando, setFalando] = useState({})
  const sessao = useRef(null)
  const estadoRef = useRef(estado)
  estadoRef.current = estado

  const atualizar = useCallback(() => setVersao((v) => v + 1), [])

  const sair = useCallback(() => {
    if (sessao.current) tocar('vozSair')
    salaDeVoz.canalId = null
    sessao.current?.sair()
    sessao.current = null
    setEstado((e) => ({ ...inicial, mudo: e.mudo, surdo: e.surdo }))
    setFalando({})
  }, [])

  const entrar = useCallback(
    async (servidorId, canalId) => {
      if (sessao.current?.canalId === canalId) return
      sessao.current?.sair()
      const atual = estadoRef.current
      const s = new Sessao((evento) => {
        if (sessao.current !== s) return
        if (evento === 'caiu') {
          const erro = s.erro || 'A conexão com a sala caiu.'
          s.sair()
          sessao.current = null
          salaDeVoz.canalId = null
          tocar('erro')
          setEstado((e) => ({ ...inicial, mudo: e.mudo, surdo: e.surdo, estado: 'erro', erro }))
          return
        }
        atualizar()
      })
      s.modoTela = atual.modoTela
      sessao.current = s
      setEstado({ ...inicial, mudo: atual.mudo, surdo: atual.surdo, modoTela: atual.modoTela, servidorId, canalId, estado: 'conectando' })
      try {
        await s.entrar(servidorId, canalId, { mudo: atual.mudo, surdo: atual.surdo })
        if (sessao.current !== s) return
        setEstado((e) => ({ ...e, estado: 'conectada', semMicrofone: !!s.semMicrofone, motivoSemMicrofone: s.motivoSemMicrofone || '' }))
        salaDeVoz.canalId = canalId
        tocar('vozEntrar')
      } catch (err) {
        if (sessao.current !== s) return
        s.sair()
        sessao.current = null
        setEstado({ ...inicial, estado: 'erro', erro: err.message || 'Não foi possível entrar.' })
      }
    },
    [atualizar],
  )

  const aplicar = useCallback((mudancas) => {
    const atual = estadoRef.current
    if ('mudo' in mudancas && mudancas.mudo !== atual.mudo) tocar(mudancas.mudo ? 'mudo' : 'desmudo')
    if ('surdo' in mudancas && mudancas.surdo !== atual.surdo) tocar(mudancas.surdo ? 'mudo' : 'desmudo')
    setEstado((e) => {
      const novo = { ...e, ...mudancas }
      const s = sessao.current
      if (s) {
        s.desejado = { mudo: novo.mudo, surdo: novo.surdo }
        if (s.faixas.audio) s.faixas.audio.enabled = !novo.mudo && !novo.surdo
        s.enviarEstado()
      }
      return novo
    })
  }, [])

  const alternarCamera = useCallback(async () => {
    const s = sessao.current
    if (!s) return
    try {
      if (s.faixas.camera) await s.trocarFaixa('camera', null)
      else {
        const m = await navigator.mediaDevices.getUserMedia({ video: restricoesDeCamera(preferencias().voz) })
        await s.trocarFaixa('camera', m.getVideoTracks()[0])
      }
      s.enviarEstado()
      tocar(s.faixas.camera ? 'ligar' : 'desligar')
      setEstado((e) => ({ ...e, camera: !!s.faixas.camera, erro: '' }))
    } catch {
      setEstado((e) => ({ ...e, erro: 'Não foi possível abrir a câmera. Confira a permissão do navegador.' }))
    }
  }, [])

  const alternarTela = useCallback(async () => {
    const s = sessao.current
    if (!s) return
    try {
      if (s.faixas.tela) {
        await s.trocarFaixa('telaAudio', null)
        await s.trocarFaixa('tela', null)
      } else {
        const modo = modoDaTela(s.modoTela)
        const m = await navigator.mediaDevices.getDisplayMedia({
          video: {
            frameRate: { ideal: modo.captura, max: modo.captura },
            width: { max: 1920 },
            height: { max: 1080 },
          },
          // som da tela: sem os filtros de voz, que estragam música e jogo
          audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false },
          systemAudio: 'include',
        })
        const faixa = m.getVideoTracks()[0]
        const som = m.getAudioTracks()[0] || null
        faixa.contentHint = modo.dica
        if (som) som.contentHint = 'music'
        faixa.onended = async () => {
          if (s.faixas.tela === faixa) {
            await s.trocarFaixa('telaAudio', null)
            await s.trocarFaixa('tela', null)
            s.enviarEstado()
            setEstado((e) => ({ ...e, tela: false, telaComSom: false }))
            tocar('desligar')
          }
        }
        s.codecTela = await escolherCodecTela(!!preferencias().voz.telaPotente)
        await s.trocarFaixa('tela', faixa)
        await s.trocarFaixa('telaAudio', som)
        await s.iniciarAuto(s.modoTela === 'auto')
        setEstado((e) => ({ ...e, telaComSom: !!som }))
      }
      s.enviarEstado()
      tocar(s.faixas.tela ? 'ligar' : 'desligar')
      setEstado((e) => ({ ...e, tela: !!s.faixas.tela, telaComSom: !!s.faixas.telaAudio, erro: '' }))
    } catch {
      // a pessoa cancelou a escolha da tela
    }
  }, [])

  const moderar = useCallback((usuarioId, acao) => sessao.current?.mandar('VOZ_MODERAR', { usuarioId, acao }), [])

  // Configurações de voz mudaram (microfone, supressão, portão…): aplica na hora.
  useEffect(() => {
    let antes = preferencias().voz
    return aoMudar(() => {
      const depois = preferencias().voz
      if (depois !== antes) sessao.current?.aplicarVoz(antes, depois)
      antes = depois
    })
  }, [])

  // Pressionar para falar: segura a tecla escolhida (fora de campos de texto).
  const [pttAtivo, setPttAtivo] = useState(false)
  // No programa para Windows, quem vigia a tecla é o próprio Windows: funciona com o Prosa
  // em segundo plano (jogo em tela cheia, outra janela).
  const { voz: prefsVoz } = usePreferencias()
  const querGlobal = noDesktop && estado.estado === 'conectada' && prefsVoz.modo === 'ptt'
  const [pttGlobal, setPttGlobal] = useState(false) // o programa confirmou que vigia a tecla
  useEffect(() => {
    if (!querGlobal) return
    let vivo = true
    definirPttGlobal(prefsVoz.teclaPtt).then((ok) => vivo && setPttGlobal(!!ok))
    const parar = aoPttGlobal((v) => {
      sessao.current?.proc?.setPtt(v)
      setPttAtivo(v)
    })
    return () => {
      vivo = false
      setPttGlobal(false)
      parar()
      definirPttGlobal('')
    }
  }, [querGlobal, prefsVoz.teclaPtt])
  useEffect(() => {
    // a tecla dentro da janela vale sempre, a não ser que o programa já esteja vigiando
    if (estado.estado !== 'conectada' || pttGlobal) return
    const digitando = (el) => el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
    const mudarPtt = (v) => {
      sessao.current?.proc?.setPtt(v)
      setPttAtivo(v)
    }
    const baixo = (e) => {
      const voz = preferencias().voz
      if (voz.modo !== 'ptt' || e.code !== voz.teclaPtt || e.repeat || digitando(document.activeElement)) return
      e.preventDefault()
      mudarPtt(true)
    }
    const cima = (e) => {
      if (e.code === preferencias().voz.teclaPtt) mudarPtt(false)
    }
    const soltar = () => mudarPtt(false)
    window.addEventListener('keydown', baixo)
    window.addEventListener('keyup', cima)
    window.addEventListener('blur', soltar)
    return () => {
      window.removeEventListener('keydown', baixo)
      window.removeEventListener('keyup', cima)
      window.removeEventListener('blur', soltar)
    }
  }, [estado.estado, pttGlobal])

  const mudarModoTela = useCallback(async (modo) => {
    try {
      localStorage.setItem('prosa.modoTela', modo)
    } catch {
      // sem armazenamento
    }
    setEstado((e) => ({ ...e, modoTela: modo }))
    await sessao.current?.mudarModoTela(modo)
  }, [])

  // quem está falando (o React só redesenha quando alguém começa ou para de falar)
  useEffect(() => {
    if (estado.estado !== 'conectada') return
    const t = setInterval(() => {
      const f = sessao.current?.falando() || {}
      setFalando((antes) => (JSON.stringify(antes) === JSON.stringify(f) ? antes : f))
    }, 60)
    return () => clearInterval(t)
  }, [estado.estado])

  useEffect(() => () => sessao.current?.sair(), [])

  const s = sessao.current
  // recalculado a cada mudança (as faixas chegam fora do React e avisam com atualizar())
  // Fechar ou recarregar a aba sai da sala na hora (sem esperar o servidor perceber).
  useEffect(() => {
    const fechando = () => sessao.current?.sair()
    window.addEventListener('pagehide', fechando)
    return () => window.removeEventListener('pagehide', fechando)
  }, [])

  // Programa para Windows: "Sair da sala de voz" no ícone ao lado do relógio.
  useEffect(() => aoSairDaVozDesktop(() => sair()), [sair])

  // Alguns navegadores (e o WebView do Windows) só tocam som depois de um clique.
  const [somBloqueado, setSomBloqueado] = useState(false)
  useEffect(() => {
    const bloqueou = () => setSomBloqueado(true)
    window.addEventListener('prosa:som-bloqueado', bloqueou)
    return () => window.removeEventListener('prosa:som-bloqueado', bloqueou)
  }, [])
  const liberarSom = useCallback(() => {
    setSomBloqueado(false)
    contextoSaida?.resume().catch(() => {})
    for (const a of document.querySelectorAll('audio.voz-audio')) a.play().catch(() => {})
  }, [])

  const valor = {
    ...estado,
    somBloqueado,
    liberarSom,
    falando,
    remotos: s?.remotos || {},
    modo: s?.modo || null,
    infoTela: s?.infoTela || '',
    infoRecebida: s?.infoRecebida || {},
    local: s?.locais || { camera: null, tela: null },
    entrar,
    sair,
    aplicar,
    alternarCamera,
    alternarTela,
    moderar,
    mudarModoTela,
    pttAtivo,
  }

  return (
    <Contexto.Provider value={valor}>
      {children}
      {/* o áudio dos outros toca mesmo navegando pelos canais de texto */}
      <div hidden>
        {Object.entries(s?.remotos || {}).map(([id, r]) => (
          <span key={id}>
            {r.audio && <Audio stream={r.audio} mudo={estado.surdo || s.bloqueado(id, 'audio')} ganho={ganhoDe(prefs.voz, id)} />}
            {r.telaAudio && <Audio stream={r.telaAudio} mudo={estado.surdo || s.bloqueado(id, 'telaAudio')} ganho={ganhoDe(prefs.voz, `t${id}`)} />}
          </span>
        ))}
      </div>
    </Contexto.Provider>
  )
}

// Um AudioContext para todos os volumes acima de 100% (o <audio> só vai até 100%).
let contextoSaida = null

// Tenta tocar; se o navegador bloquear (falta de clique), avisa para mostrar o botão "Ativar som".
function tocarOuAvisar(el) {
  const p = el.play?.()
  p?.catch?.((e) => e?.name === 'NotAllowedError' && window.dispatchEvent(new Event('prosa:som-bloqueado')))
}

/**
 * Toca a voz de alguém. Até 100% usa o volume do próprio <audio>; acima disso passa por
 * um ganho do Web Audio. O <audio> mudo com o som original continua existindo porque o
 * Chrome só entrega áudio do WebRTC ao Web Audio se ele também estiver tocando num elemento.
 */
function Audio({ stream, mudo, ganho = 1 }) {
  const ref = useRef(null)
  const bruto = useRef(null)
  const ganhoNo = useRef(null)
  const { voz } = usePreferencias()
  const total = Math.max(0, (voz.volumeSaida ?? 1) * ganho)
  const amplificar = total > 1

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (!amplificar) {
      if (el.srcObject !== stream) el.srcObject = stream
      ganhoNo.current = null
      tocarOuAvisar(el)
      return
    }
    try {
      contextoSaida ||= new AudioContext()
      contextoSaida.resume().catch(() => {})
      const fonte = contextoSaida.createMediaStreamSource(stream)
      const g = contextoSaida.createGain()
      const destino = contextoSaida.createMediaStreamDestination()
      fonte.connect(g).connect(destino)
      ganhoNo.current = g
      el.srcObject = destino.stream
      tocarOuAvisar(el)
      if (bruto.current) bruto.current.srcObject = stream
      return () => {
        fonte.disconnect()
        g.disconnect()
        ganhoNo.current = null
      }
    } catch {
      el.srcObject = stream // sem Web Audio: fica no máximo de 100%
    }
  }, [stream, amplificar])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (ganhoNo.current) {
      ganhoNo.current.gain.value = total
      el.volume = 1
    } else el.volume = Math.min(1, total)
  }, [total, amplificar])

  useEffect(() => {
    const el = ref.current
    if (el?.setSinkId && voz.saida !== undefined) el.setSinkId(voz.saida || '').catch(() => {})
  }, [voz.saida])

  return (
    <>
      <audio ref={ref} className="voz-audio" autoPlay muted={mudo || total === 0} />
      {amplificar && <audio ref={bruto} autoPlay muted />}
    </>
  )
}

export const useVoz = () => useContext(Contexto)
