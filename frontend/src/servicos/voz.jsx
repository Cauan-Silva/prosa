// Voz: uma conexão WebRTC com o servidor de mídia (SFU) do Prosa.
//
// O servidor manda as ofertas; o navegador só responde. As três primeiras faixas da
// conexão são as de envio (microfone, câmera e tela); o que chega dos outros vem em
// faixas novas, identificadas pelo id do "stream" (u<id da pessoa>).
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, lerToken } from './api.js'

const Contexto = createContext(null)

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
    this.faixas = { audio: null, camera: null, tela: null }
    this.locais = { camera: null, tela: null } // prévias do que eu mando
    this.remotos = {} // usuarioId -> { audio, camera, tela } (MediaStream)
    this.mids = {}
    this.fila = Promise.resolve()
    this.niveis = {}
    this.fechada = false
  }

  async entrar(servidorId, canalId, desejado) {
    this.servidorId = servidorId
    this.canalId = canalId
    this.desejado = desejado
    const info = await api.vozInfo(canalId)
    this.ice = info.ice
    try {
      const m = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
      this.faixas.audio = m.getAudioTracks()[0]
      this.faixas.audio.enabled = !desejado.mudo
      this.medir('eu', m)
    } catch {
      this.semMicrofone = true // entra só para ouvir
    }
    await this.conectar(info.endereco, 0)
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
          this.criarConexao(d.ice || this.ice)
          this.enviarEstado()
          resolver()
        } else if (t === 'VOZ_REDIRECIONAR' && tentativa < 2) {
          ws.onclose = null
          this.conectar(d.endereco, tentativa + 1).then(resolver, rejeitar)
        } else if (t === 'VOZ_OFERTA') {
          this.fila = this.fila.then(() => this.responder(d)).catch((e) => console.warn('voz: oferta', e))
        } else if (t === 'VOZ_ICE') {
          this.fila = this.fila.then(() => this.pc?.addIceCandidate(d.candidato)).catch(() => {})
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

  criarConexao(ice) {
    const pc = new RTCPeerConnection({ iceServers: ice })
    this.pc = pc
    pc.onicecandidate = (e) => e.candidate && this.mandar('VOZ_ICE', { candidato: e.candidate.toJSON() })
    pc.onconnectionstatechange = () => {
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
      const tipo = ['audio', 'camera', 'tela'].includes(e.track.id) ? e.track.id : e.track.kind === 'audio' ? 'audio' : 'camera'
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

  async responder({ sdp, mids }) {
    const pc = this.pc
    this.mids = mids
    await pc.setRemoteDescription({ type: 'offer', sdp })
    for (const t of pc.getTransceivers()) {
      const tipo = mids[t.mid]
      if (!tipo) continue
      if (t.direction !== 'sendonly') t.direction = 'sendonly'
      const faixa = this.faixas[tipo]
      if (t.sender.track !== faixa) await t.sender.replaceTrack(faixa)
    }
    const resposta = await pc.createAnswer()
    await pc.setLocalDescription(resposta)
    this.mandar('VOZ_RESPOSTA', { sdp: pc.localDescription.sdp })
  }

  transceptor(tipo) {
    return this.pc?.getTransceivers().find((t) => this.mids[t.mid] === tipo)
  }

  async trocarFaixa(tipo, faixa) {
    const antiga = this.faixas[tipo]
    if (antiga && antiga !== faixa) antiga.stop()
    this.faixas[tipo] = faixa
    if (tipo !== 'audio') this.locais[tipo] = faixa ? new MediaStream([faixa]) : null
    await this.transceptor(tipo)?.sender.replaceTrack(faixa)
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
    this.pc?.close()
    for (const f of Object.values(this.faixas)) f?.stop()
    this.audio?.close().catch(() => {})
  }
}

export function VozProvider({ children }) {
  const [estado, setEstado] = useState(inicial)
  const [, setVersao] = useState(0)
  const [falando, setFalando] = useState({})
  const sessao = useRef(null)
  const estadoRef = useRef(estado)
  estadoRef.current = estado

  const atualizar = useCallback(() => setVersao((v) => v + 1), [])

  const sair = useCallback(() => {
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
          setEstado((e) => ({ ...inicial, mudo: e.mudo, surdo: e.surdo, estado: 'erro', erro }))
          return
        }
        atualizar()
      })
      sessao.current = s
      setEstado({ ...inicial, mudo: atual.mudo, surdo: atual.surdo, servidorId, canalId, estado: 'conectando' })
      try {
        await s.entrar(servidorId, canalId, { mudo: atual.mudo, surdo: atual.surdo })
        if (sessao.current !== s) return
        setEstado((e) => ({ ...e, estado: 'conectada', semMicrofone: !!s.semMicrofone }))
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
        const m = await navigator.mediaDevices.getUserMedia({ video: { width: 640, height: 360, frameRate: 24 } })
        await s.trocarFaixa('camera', m.getVideoTracks()[0])
      }
      s.enviarEstado()
      setEstado((e) => ({ ...e, camera: !!s.faixas.camera, erro: '' }))
    } catch {
      setEstado((e) => ({ ...e, erro: 'Não foi possível abrir a câmera. Confira a permissão do navegador.' }))
    }
  }, [])

  const alternarTela = useCallback(async () => {
    const s = sessao.current
    if (!s) return
    try {
      if (s.faixas.tela) await s.trocarFaixa('tela', null)
      else {
        const m = await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: 15 }, audio: false })
        const faixa = m.getVideoTracks()[0]
        faixa.contentHint = 'detail'
        faixa.onended = async () => {
          if (s.faixas.tela === faixa) {
            await s.trocarFaixa('tela', null)
            s.enviarEstado()
            setEstado((e) => ({ ...e, tela: false }))
          }
        }
        await s.trocarFaixa('tela', faixa)
      }
      s.enviarEstado()
      setEstado((e) => ({ ...e, tela: !!s.faixas.tela, erro: '' }))
    } catch {
      // a pessoa cancelou a escolha da tela
    }
  }, [])

  const moderar = useCallback((usuarioId, acao) => sessao.current?.mandar('VOZ_MODERAR', { usuarioId, acao }), [])

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
  const valor = {
    ...estado,
    falando,
    remotos: s?.remotos || {},
    local: s?.locais || { camera: null, tela: null },
    entrar,
    sair,
    aplicar,
    alternarCamera,
    alternarTela,
    moderar,
  }

  return (
    <Contexto.Provider value={valor}>
      {children}
      {/* o áudio dos outros toca mesmo navegando pelos canais de texto */}
      <div hidden>
        {Object.entries(s?.remotos || {}).map(([id, r]) => r.audio && <Audio key={id} stream={r.audio} mudo={estado.surdo} />)}
      </div>
    </Contexto.Provider>
  )
}

function Audio({ stream, mudo }) {
  const ref = useRef(null)
  useEffect(() => {
    if (ref.current && ref.current.srcObject !== stream) ref.current.srcObject = stream
  }, [stream])
  return <audio ref={ref} autoPlay muted={mudo} />
}

export const useVoz = () => useContext(Contexto)
