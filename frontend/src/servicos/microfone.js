// Microfone: pega o áudio com as opções escolhidas (supressão de ruído, eco, ganho) e,
// se ligado, passa por um "portão" que só deixa a voz passar quando ela fica acima da
// sensibilidade — o ruído de fundo (ventilador, teclado) some nos intervalos da fala.
// Também é por aqui que o "pressionar para falar" abre e fecha o microfone.
import { preferencias } from './preferencias.js'

export function restricoesDeAudio(voz = preferencias().voz) {
  return {
    deviceId: voz.microfone ? { exact: voz.microfone } : undefined,
    echoCancellation: voz.cancelamentoEco,
    noiseSuppression: voz.supressaoRuido,
    autoGainControl: voz.ganhoAutomatico,
    channelCount: 1,
  }
}

export async function abrirMicrofone(voz = preferencias().voz) {
  try {
    return await navigator.mediaDevices.getUserMedia({ audio: restricoesDeAudio(voz) })
  } catch (e) {
    // o microfone escolhido sumiu (desconectado): tenta o padrão
    if (voz.microfone && (e.name === 'OverconstrainedError' || e.name === 'NotFoundError')) {
      return navigator.mediaDevices.getUserMedia({ audio: restricoesDeAudio({ ...voz, microfone: '' }) })
    }
    throw e
  }
}

export class Processador {
  constructor(stream, opcoes) {
    this.original = stream
    this.ctx = new AudioContext()
    const fonte = this.ctx.createMediaStreamSource(stream)
    // corta o zumbido grave (rede elétrica, vento) antes de tudo
    const passaAlta = this.ctx.createBiquadFilter()
    passaAlta.type = 'highpass'
    passaAlta.frequency.value = 85
    this.analisador = this.ctx.createAnalyser()
    this.analisador.fftSize = 1024
    this.portao = this.ctx.createGain()
    this.portao.gain.value = 1
    const saida = this.ctx.createMediaStreamDestination()
    fonte.connect(passaAlta)
    passaAlta.connect(this.analisador)
    passaAlta.connect(this.portao).connect(saida)
    // nível depois do portão (para mostrar quem está falando)
    this.analisadorSaida = this.ctx.createAnalyser()
    this.analisadorSaida.fftSize = 512
    this.portao.connect(this.analisadorSaida)
    this.track = saida.stream.getAudioTracks()[0]
    this.stream = saida.stream
    this.dados = new Float32Array(this.analisador.fftSize)
    this.dadosSaida = new Float32Array(this.analisadorSaida.fftSize)
    this.aberto = true
    this.ultimaVoz = 0
    this.pttApertado = false
    this.opcoes = opcoes
    this.timer = setInterval(() => this.passo(), 20)
  }

  setOpcoes(opcoes) {
    this.opcoes = { ...this.opcoes, ...opcoes }
  }

  setPtt(apertado) {
    this.pttApertado = apertado
    this.passo()
  }

  /** Volume de entrada em dBFS (-100 = silêncio, 0 = máximo), antes do portão. */
  nivel() {
    this.analisador.getFloatTimeDomainData(this.dados)
    let soma = 0
    for (const v of this.dados) soma += v * v
    const rms = Math.sqrt(soma / this.dados.length)
    return rms > 0 ? Math.max(-100, 20 * Math.log10(rms)) : -100
  }

  nivelSaida() {
    this.analisadorSaida.getFloatTimeDomainData(this.dadosSaida)
    let soma = 0
    for (const v of this.dadosSaida) soma += v * v
    return Math.sqrt(soma / this.dadosSaida.length)
  }

  passo() {
    const { modo, filtroRuido, sensibilidade } = this.opcoes
    let abrir = true
    if (modo === 'ptt') abrir = this.pttApertado
    else if (filtroRuido) {
      const agora = performance.now()
      if (this.nivel() > sensibilidade) this.ultimaVoz = agora
      abrir = agora - this.ultimaVoz < 300 // segura 300 ms para não cortar o fim das palavras
    }
    if (abrir !== this.aberto) {
      this.aberto = abrir
      const t = this.ctx.currentTime
      this.portao.gain.cancelScheduledValues(t)
      this.portao.gain.setTargetAtTime(abrir ? 1 : 0, t, abrir ? 0.005 : 0.04)
    }
  }

  destruir() {
    clearInterval(this.timer)
    for (const t of this.original.getTracks()) t.stop()
    this.track.stop()
    this.ctx.close().catch(() => {})
  }
}
