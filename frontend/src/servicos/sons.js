// Sons da interface, sintetizados na hora com a Web Audio API (sem arquivos de áudio).
// Cada som é uma sequência curta de notas; dá para desligar e ajustar o volume no perfil.

const CHAVE = 'prosa.sons'

function lerConfig() {
  try {
    return { ligado: true, volume: 0.6, ...JSON.parse(localStorage.getItem(CHAVE) || '{}') }
  } catch {
    return { ligado: true, volume: 0.6 }
  }
}

let config = lerConfig()
let contexto = null

export const configSons = () => config

export function mudarSons(mudancas) {
  config = { ...config, ...mudancas }
  try {
    localStorage.setItem(CHAVE, JSON.stringify(config))
  } catch {
    // sem armazenamento: vale até fechar a aba
  }
}

// notas: [frequência em Hz, início em s, duração em s, volume relativo, forma da onda]
const SONS = {
  mensagem: [[660, 0, 0.09, 0.5, 'sine'], [880, 0.07, 0.12, 0.4, 'sine']],
  mencao: [[784, 0, 0.1, 0.6, 'triangle'], [988, 0.09, 0.1, 0.6, 'triangle'], [1319, 0.18, 0.22, 0.5, 'triangle']],
  enviar: [[520, 0, 0.05, 0.25, 'sine'], [780, 0.03, 0.06, 0.2, 'sine']],
  reacao: [[1046, 0, 0.06, 0.35, 'sine']],
  vozEntrar: [[440, 0, 0.12, 0.5, 'sine'], [660, 0.1, 0.18, 0.5, 'sine']],
  vozSair: [[660, 0, 0.12, 0.5, 'sine'], [392, 0.1, 0.2, 0.5, 'sine']],
  entrou: [[587, 0, 0.1, 0.35, 'sine'], [784, 0.08, 0.14, 0.35, 'sine']],
  saiu: [[784, 0, 0.1, 0.35, 'sine'], [523, 0.08, 0.16, 0.35, 'sine']],
  mudo: [[330, 0, 0.07, 0.45, 'square']],
  desmudo: [[494, 0, 0.07, 0.4, 'square']],
  ligar: [[523, 0, 0.07, 0.35, 'triangle'], [784, 0.06, 0.1, 0.35, 'triangle']],
  desligar: [[784, 0, 0.07, 0.35, 'triangle'], [523, 0.06, 0.1, 0.35, 'triangle']],
  erro: [[220, 0, 0.18, 0.45, 'sawtooth']],
}

const ultimaVez = {}

/** Toca um som pelo nome. Sons iguais em menos de 150 ms viram um só. */
export function tocar(nome) {
  const notas = SONS[nome]
  if (!notas || !config.ligado || config.volume <= 0) return
  const agoraMs = performance.now()
  if (agoraMs - (ultimaVez[nome] || 0) < 150) return
  ultimaVez[nome] = agoraMs
  try {
    contexto ||= new AudioContext()
    if (contexto.state === 'suspended') contexto.resume()
    const t0 = contexto.currentTime + 0.01
    const saida = contexto.createGain()
    saida.gain.value = config.volume * 0.35
    saida.connect(contexto.destination)
    for (const [freq, inicio, duracao, vol, onda] of notas) {
      const osc = contexto.createOscillator()
      const env = contexto.createGain()
      osc.type = onda
      osc.frequency.value = freq
      const a = t0 + inicio
      env.gain.setValueAtTime(0, a)
      env.gain.linearRampToValueAtTime(vol, a + 0.008)
      env.gain.exponentialRampToValueAtTime(0.0001, a + duracao)
      osc.connect(env).connect(saida)
      osc.start(a)
      osc.stop(a + duracao + 0.02)
    }
  } catch {
    // navegador sem Web Audio: fica em silêncio
  }
}

// Sala de voz em que a pessoa está agora (a voz avisa; o gateway usa para tocar entrou/saiu).
export const salaDeVoz = { canalId: null }
