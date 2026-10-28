// Enfeites do perfil: moldura em volta da foto, efeito animado no cartão e estilo do nome.
import { memo } from 'react'
import { hex } from '../servicos/visual.js'

// "aleatório" fixo por posição: as partículas não pulam de lugar a cada nova renderização
const sorteio = (i, s = 1) => {
  const x = Math.sin(i * 12.9898 * s + 78.233) * 43758.5453
  return x - Math.floor(x)
}

const FOLHA = 'M12 2C6 6 4 12 6 20c8-1 13-7 14-15-3-1-6-2-8-3zM6 20 15 8'
const ESTRELA = 'M12 1.5 14.6 9.4 22.5 12 14.6 14.6 12 22.5 9.4 14.6 1.5 12 9.4 9.4z'

// anel de "pixels": quadradinhos numa grade 20x20 entre dois raios
const PIXELS = (() => {
  const lista = []
  for (let y = 0; y < 20; y++)
    for (let x = 0; x < 20; x++) {
      const d = Math.hypot(x + 0.5 - 10, y + 0.5 - 10)
      if (d >= 9 && d < 10) lista.push([x, y, (x + y) % 3])
    }
  return lista
})()
const CORES_PIXEL = ['#7c5cff', '#a855f7', '#4fd1ff']
function AnelPixel() {
  return (
    <svg className="moldura-pixels" viewBox="0 0 20 20" shapeRendering="crispEdges">
      {PIXELS.map(([x, y, c]) => (
        <rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" fill={CORES_PIXEL[c]} />
      ))}
    </svg>
  )
}

/** Desenho que fica por cima da foto (o .avatar posiciona). */
export const Moldura = memo(function Moldura({ id }) {
  if (!id) return null
  let extra = null
  if (id === 'folhas')
    extra = [20, 130, 250, 320].map((ang, i) => (
      <svg key={i} className="moldura-peca" viewBox="0 0 24 24" style={{ '--ang': `${ang}deg`, '--esc': i % 2 ? 0.8 : 1 }}>
        <path d={FOLHA} />
      </svg>
    ))
  else if (id === 'estrelas')
    extra = [35, 160, 290].map((ang, i) => (
      <svg key={i} className="moldura-peca" viewBox="0 0 24 24" style={{ '--ang': `${ang}deg`, '--atraso': `${i * 0.6}s` }}>
        <path d={ESTRELA} />
      </svg>
    ))
  else if (id === 'chamas')
    // labaredas em volta, subindo; maiores embaixo
    extra = Array.from({ length: 9 }, (_, i) => {
      const ang = ((70 + i * 27.5) * Math.PI) / 180
      return (
        <i
          key={i}
          className="moldura-chama"
          style={{
            left: `${50 + 47 * Math.sin(ang)}%`,
            top: `${50 - 47 * Math.cos(ang)}%`,
            '--esc': (0.75 + 0.5 * Math.max(0, -Math.cos(ang))).toFixed(2),
            '--atraso': `${(sorteio(i) * -1.2).toFixed(2)}s`,
          }}
        />
      )
    })
  else if (id === 'pixel') extra = <AnelPixel />
  else if (id === 'orbita') extra = <i className="moldura-planeta" />
  else if (id === 'fones')
    extra = (
      <svg className="moldura-fones-svg" viewBox="0 0 100 100">
        <path d="M14 58a36 36 0 0 1 72 0" fill="none" strokeWidth="5" strokeLinecap="round" />
        <rect x="4" y="50" width="16" height="30" rx="7" />
        <rect x="80" y="50" width="16" height="30" rx="7" />
      </svg>
    )
  else if (id === 'coroa')
    extra = (
      <svg className="moldura-coroa-svg" viewBox="0 0 60 40">
        <path d="M4 34 8 8l13 14L30 2l9 20L52 8l4 26z" />
        <circle cx="30" cy="27" r="3.4" />
      </svg>
    )
  return (
    <span className={`moldura moldura-${id}`} aria-hidden="true">
      {extra}
    </span>
  )
})

const QTD = { confete: 26, neve: 34, estrelas: 18, bolhas: 16, petalas: 18, coracoes: 14, vagalumes: 16, fogos: 5, codigo: 14 }
const CORES_CONFETE = ['#ffd84d', '#ff5d8f', '#4fd1ff', '#7c5cff', '#3fbf7f', '#ff9f43']

/** Animação por cima do cartão de perfil. */
export const Efeito = memo(function Efeito({ id }) {
  if (!id) return null
  if (id === 'aurora')
    return (
      <span className="efeito efeito-aurora" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
    )
  const n = QTD[id] || 12
  return (
    <span className={`efeito efeito-${id}`} aria-hidden="true">
      {Array.from({ length: n }, (_, i) => {
        const st = {
          '--x': `${(sorteio(i, 1) * 100).toFixed(1)}%`,
          '--y': `${(sorteio(i, 2) * 100).toFixed(1)}%`,
          '--t': `${(0.6 + sorteio(i, 3)).toFixed(2)}`,
          '--atraso': `${(-sorteio(i, 4) * 8).toFixed(2)}s`,
          '--cor': CORES_CONFETE[i % CORES_CONFETE.length],
          '--giro': `${Math.round(sorteio(i, 5) * 360)}deg`,
        }
        if (id === 'codigo')
          return (
            <i key={i} style={st}>
              {Array.from({ length: 8 }, (_, k) => (sorteio(i * 9 + k, 6) > 0.5 ? '1' : '0')).join('\n')}
            </i>
          )
        if (id === 'fogos')
          return (
            <i key={i} style={st}>
              {Array.from({ length: 10 }, (_, k) => (
                <b key={k} style={{ '--k': `${k * 36}deg` }} />
              ))}
            </i>
          )
        return <i key={i} style={st} />
      })}
    </span>
  )
})

/** Nome com o estilo escolhido (usa as cores do tema quando houver). */
export function NomeEstilizado({ estilo, tema, children, como: Tag = 'span', className = '' }) {
  if (!estilo) return <Tag className={className}>{children}</Tag>
  const [a, b] = tema?.length === 2 ? tema.map(hex) : ['#5b8cff', '#ff5d8f']
  return (
    <Tag className={`${className} nome-estilo nome-${estilo}`} style={{ '--n1': a, '--n2': b }} data-texto={typeof children === 'string' ? children : undefined}>
      {children}
    </Tag>
  )
}
