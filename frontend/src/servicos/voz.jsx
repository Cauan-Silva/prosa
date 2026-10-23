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
const tipoDeMidia = (tipo) => (tipo === 'audio' || tipo === 'telaAudio' ? 'audio' : 'video')

// Modos da tela compartilhada: quadros por segundo capturados e enviados, e quanta banda
// cada cópia pode usar (no modo direto o total é dividido entre quem assiste).
// "fps60" baixa a resolução antes de perder quadros: bom para jogos e vídeos.
const MODOS_TELA = {
  nitido: { captura: 15, maxFramerate: 10, maxBitrate: 1_200_000, total: 3_000_000, minimo: 500_000, preferencia: 'maintain-resolution', dica: 'detail' },
  fluido: { captura: 30, maxFramerate: 30, maxBitrate: 2_000_000, total: 4_000_000, minimo: 700_000, preferencia: 'maintain-framerate', dica: 'motion' },
  fps60: { captura: 60, maxFramerate: 60, maxBitrate: 4_500_000, total: 8_000_000, minimo: 1_500_000, preferencia: 'maintain-framerate', dica: 'motion' },
}
const modoDaTela = (modo) => MODOS_TELA[modo] || MODOS_TELA.fluido

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
  modoTela: (() => {
    try {
      return localStorage.getItem('prosa.modoTela') || 'fluido'
    } catch {
      return 'fluido'
    }
  })(),
}

function enderecoVoz(base, canalId) {
  const origem = base || location.origin
  const u = new URL(`/api/voz/${canalId}`, origem)
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:'
  u.searchParams.set('t', lerToken() || '')
  return u.toString()
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
    this.modoTela = 'fluido'
  }

  async entrar(servidorId, canalId, desejado) {
    this.servidorId = servidorId
    this.canalId = canalId
    this.desejado = desejado
    const info = await api.vozInfo(canalId)
    this.ice = info.ice
    try {
      await this.abrirMicrofone()
    } catch {
      this.semMicrofone = true // entra só para ouvir
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
    this.niveis.eu = () => novo.nivelSaida() * 128
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
      for (const tipo of TIPOS) pc.addTransceiver(this.faixas[tipo] || tipoDeMidia(tipo), { direction: 'sendrecv' })
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
      .then(() => (s.tipo === 'resposta' ? par.pc.setRemoteDescription({ type: 'answer', sdp: s.sdp }) : par.pc.addIceCandidate(s.candidato)))
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
        return { maxBitrate: dividir(m.maxBitrate, m.total, m.minimo), maxFramerate: m.maxFramerate, preferencia: m.preferencia }
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
          if (r.preferencia) p.degradationPreference = r.preferencia
          await envio.setParameters(p)
        } catch {
          // navegador sem suporte a algum ajuste (ou conexão ainda negociando): segue com o padrão
        }
      }
    }
  }

  async mudarModoTela(modo) {
    this.modoTela = modo
    const faixa = this.faixas.tela
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

  async trocarFaixa(tipo, faixa) {
    const antiga = this.faixas[tipo]
    if (antiga && antiga !== faixa) antiga.stop()
    this.faixas[tipo] = faixa
    if (tipo === 'camera' || tipo === 'tela') this.locais[tipo] = faixa ? new MediaStream([faixa]) : null
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

  // Mede o volume de cada voz para mostrar quem está falando.
  medir(chave, stream) {
    try {
      this.audio ||= new AudioContext()
      const fonte = this.audio.createMediaStreamSource(stream)
      const analisador = this.audio.createAnalyser()
      analisador.fftSize = 512
      fonte.connect(analisador)
      const dados = new Uint8Array(analisador.fftSize)
      this.niveis[chave] = () => {
        analisador.getByteTimeDomainData(dados)
        let soma = 0
        for (const v of dados) soma += (v - 128) ** 2
        return Math.sqrt(soma / dados.length)
      }
    } catch {
      // sem Web Audio: só não mostra quem fala
    }
  }

  falando() {
    const saida = {}
    for (const [chave, nivel] of Object.entries(this.niveis)) if (nivel() > 6) saida[chave] = true
    return saida
  }

  sair() {
    this.fechada = true
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
        setEstado((e) => ({ ...e, estado: 'conectada', semMicrofone: !!s.semMicrofone }))
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
        await s.trocarFaixa('tela', faixa)
        await s.trocarFaixa('telaAudio', som)
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

  // quem está falando (atualiza poucas vezes por segundo)
  useEffect(() => {
    if (estado.estado !== 'conectada') return
    const t = setInterval(() => {
      const f = sessao.current?.falando() || {}
      setFalando((antes) => (JSON.stringify(antes) === JSON.stringify(f) ? antes : f))
    }, 250)
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
