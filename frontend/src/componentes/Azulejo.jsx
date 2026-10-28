import { Moldura } from './Visual.jsx'

// Cada servidor ganha um azulejo próprio, desenhado a partir do nome: o mesmo nome
// sempre gera o mesmo desenho. É a "cara" do servidor na barra lateral.

function semente(texto) {
  let h = 2166136261
  for (const ch of texto) {
    h ^= ch.codePointAt(0)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

// Paletas inspiradas em azulejos: fundo claro e duas tintas.
const tintas = [
  ['#1f3fa6', '#e8b420'],
  ['#14635a', '#e8b420'],
  ['#1f3fa6', '#c9472f'],
  ['#5a2d82', '#e8b420'],
  ['#14635a', '#1f3fa6'],
  ['#9c2f4a', '#1f3fa6'],
]

// Motivos de um quarto de azulejo (o desenho é espelhado nos quatro cantos).
const motivos = [
  (a, b) => (
    <>
      <path d="M0 0h20v20H0z" fill="none" />
      <path d="M0 0 20 20" stroke={a} strokeWidth="5" />
      <circle cx="20" cy="20" r="6" fill={b} />
    </>
  ),
  (a, b) => (
    <>
      <path d="M0 0h12a8 8 0 0 1-8 8H0z" fill={a} />
      <path d="M20 8a12 12 0 0 0-12 12h12z" fill={b} />
    </>
  ),
  (a, b) => (
    <>
      <path d="M0 10 10 0h10L0 20z" fill={a} />
      <rect x="14" y="14" width="6" height="6" fill={b} />
    </>
  ),
  (a, b) => (
    <>
      <circle cx="0" cy="0" r="12" fill="none" stroke={a} strokeWidth="4" />
      <path d="M20 12 12 20h8z" fill={b} />
    </>
  ),
  (a, b) => (
    <>
      <path d="M0 4h4V0h4v8H0z" fill={a} />
      <path d="M20 20h-9c0-5 4-9 9-9z" fill={b} />
      <path d="M8 20 20 8" stroke={a} strokeWidth="2" />
    </>
  ),
]

export function Azulejo({ nome, tamanho = 48, src }) {
  // imagem escolhida para o servidor; sem ela, o azulejo desenhado a partir do nome
  if (src) return <img className="azulejo azulejo-imagem" src={src} alt="" width={tamanho} height={tamanho} draggable="false" />
  const h = semente(nome || '?')
  const [a, b] = tintas[h % tintas.length]
  const motivo = motivos[(h >>> 4) % motivos.length]
  // um pouco maior que o quarto para as quatro partes se sobreporem e não aparecer emenda
  const quarto = <g transform="scale(1.012)">{motivo(a, b)}</g>
  return (
    <svg className="azulejo" width={tamanho} height={tamanho} viewBox="0 0 40 40" aria-hidden="true">
      <rect width="40" height="40" fill="var(--azulejo-fundo)" />
      <g>{quarto}</g>
      <g transform="translate(40 0) scale(-1 1)">{quarto}</g>
      <g transform="translate(0 40) scale(1 -1)">{quarto}</g>
      <g transform="translate(40 40) scale(-1 -1)">{quarto}</g>
    </svg>
  )
}

/** Iniciais para quando o azulejo é pequeno demais (ou para avatares de pessoas). */
export function iniciais(nome) {
  const p = (nome || '?').trim().split(/\s+/)
  return ((p[0]?.[0] || '') + (p.length > 1 ? p.at(-1)[0] : '')).toUpperCase()
}

const coresAvatar = ['#1f3fa6', '#14635a', '#9c2f4a', '#5a2d82', '#b5540f', '#2b6f8f', '#7a5a00', '#3d4a6b']

export function Avatar({ nome, id, tamanho = 36, online, src, status, moldura }) {
  let h = semente(`pessoa:${id ?? nome}`)
  h = Math.imul(h ^ (h >>> 15), 0x2c1b3c6d) >>> 0
  const a = coresAvatar[(h >>> 7) % coresAvatar.length]
  const ponto = online === undefined ? null : !online || status === 'invisivel' ? 'off' : status || 'online'
  return (
    <span className={`avatar ${moldura && tamanho >= 28 ? 'com-moldura' : ''}`} style={{ width: tamanho, height: tamanho, background: src ? 'var(--linha)' : a, fontSize: tamanho * 0.38, '--tam': `${tamanho}px` }}>
      {src ? <img src={src} alt="" loading="lazy" draggable="false" /> : iniciais(nome)}
      {tamanho >= 28 && <Moldura id={moldura} />}
      {ponto && <span className={`ponto-online ${ponto}`} aria-label={ROTULOS[ponto]} title={ROTULOS[ponto]} />}
    </span>
  )
}

const ROTULOS = { online: 'online', ausente: 'ausente', ocupado: 'não perturbe', off: 'offline' }
