// Mostra o texto das mensagens: menções, links e um pouco de formatação
// (**negrito**, *itálico*, ~~riscado~~, ||spoiler||, `código` e blocos ```código```).
// Tudo vira elementos do React, nunca HTML cru, então não há como injetar código.
import { Fragment, useState } from 'react'

const URL_RE = String.raw`https?:\/\/[^\s<>"]+[^\s<>".,;:!?)\]'|]`
const padrao = new RegExp(
  String.raw`(\x60\x60\x60[\s\S]+?\x60\x60\x60)|(\x60[^\x60\n]+\x60)|(<@&?\d{1,19}>)|(\B@todos\b)|(<${URL_RE}>)|(${URL_RE})|(\|\|[\s\S]+?\|\|)|(\*\*[^*\n]+\*\*)|(~~[^~\n]+~~)|(\*[^*\n]+\*|_[^_\n]+_)|(<a?:\w{2,32}:\d{1,19}>)`,
  'g',
)

const EMOJI_PROPRIO = /<a?:\w{2,32}:\d{1,19}>/g
const SO_EMOJI = /^(?:\p{Extended_Pictographic}|\p{Emoji_Component}|\u200d|\ufe0f|\s)+$/u

/** Mensagem só com emojis (até 27): eles aparecem grandes, como no Discord. */
function soEmojis(texto) {
  const proprios = texto.match(EMOJI_PROPRIO)?.length || 0
  const resto = texto.replace(EMOJI_PROPRIO, '').trim()
  if (!resto) return proprios > 0 && proprios <= 27
  const unicode = [...new Intl.Segmenter().segment(resto)].filter((s) => s.segment.trim()).length
  return SO_EMOJI.test(resto) && /\p{Extended_Pictographic}/u.test(resto) && proprios + unicode <= 27
}

/** URL da imagem de um emoji de servidor (pelo id). */
export const urlEmoji = (id) => `/api/emojis/${id}`

/**
 * Links que ganham cartão de prévia: fora de código e de spoiler, e sem os <> em volta
 * (como no Discord, <https://...> manda o link sem prévia).
 */
export function linksComPrevia(conteudo, max = 2) {
  const limpo = conteudo
    .replace(/```[\s\S]+?```/g, ' ')
    .replace(/`[^`\n]+`/g, ' ')
    .replace(/\|\|[\s\S]+?\|\|/g, ' ')
    .replace(new RegExp(`<${URL_RE}>`, 'g'), ' ')
  const vistos = []
  for (const m of limpo.matchAll(new RegExp(URL_RE, 'g'))) {
    if (!vistos.includes(m[0])) vistos.push(m[0])
    if (vistos.length >= max) break
  }
  return vistos
}

export function Texto({ conteudo, membros, cargos, eu, aoMencionar }) {
  const jumbo = soEmojis(conteudo)
  const corpo = partes(conteudo, { membros, cargos, eu, aoMencionar, jumbo }, 0)
  return jumbo ? <span className="so-emojis">{corpo}</span> : <>{corpo}</>
}

function partes(texto, ctx, nivel) {
  const saida = []
  let ultimo = 0
  let i = 0
  for (const m of texto.matchAll(padrao)) {
    if (m.index > ultimo) saida.push(quebras(texto.slice(ultimo, m.index), i++))
    const t = m[0]
    const k = `${nivel}-${i++}`
    if (m[1]) saida.push(<pre key={k} className="bloco-codigo"><code>{t.slice(3, -3).replace(/^\w*\n/, '')}</code></pre>)
    else if (m[2]) saida.push(<code key={k}>{t.slice(1, -1)}</code>)
    else if (m[3]) saida.push(mencao(t, ctx, k))
    else if (m[4]) saida.push(<span key={k} className="mencao mencao-todos">@todos</span>)
    else if (m[5]) saida.push(<a key={k} href={t.slice(1, -1)} target="_blank" rel="noopener noreferrer nofollow">{t.slice(1, -1)}</a>)
    else if (m[6]) saida.push(<a key={k} href={t} target="_blank" rel="noopener noreferrer nofollow">{t}</a>)
    else if (nivel < 3 && m[7]) saida.push(<Spoiler key={k}>{partes(t.slice(2, -2), ctx, nivel + 1)}</Spoiler>)
    else if (nivel < 3 && m[8]) saida.push(<strong key={k}>{partes(t.slice(2, -2), ctx, nivel + 1)}</strong>)
    else if (nivel < 3 && m[9]) saida.push(<s key={k}>{partes(t.slice(2, -2), ctx, nivel + 1)}</s>)
    else if (nivel < 3 && m[10]) saida.push(<em key={k}>{partes(t.slice(1, -1), ctx, nivel + 1)}</em>)
    else if (m[11]) {
      const [, nome, id] = t.match(/<a?:(\w+):(\d+)>/)
      saida.push(<EmojiProprio key={k} id={id} nome={nome} />)
    }
    else saida.push(t)
    ultimo = m.index + t.length
  }
  if (ultimo < texto.length) saida.push(quebras(texto.slice(ultimo), i++))
  return saida
}

/** Emoji de servidor; se ele foi apagado, vira o texto :nome:. */
function EmojiProprio({ id, nome }) {
  const [sumiu, setSumiu] = useState(false)
  if (sumiu) return <span className="emoji-apagado" title="Emoji apagado">:{nome}:</span>
  return <img className="emoji-proprio" src={urlEmoji(id)} alt={`:${nome}:`} title={`:${nome}:`} loading="lazy" draggable="false" onError={() => setSumiu(true)} />
}

/** Texto escondido até alguém clicar. */
function Spoiler({ children }) {
  const [visivel, setVisivel] = useState(false)
  if (visivel) return <span className="spoiler visivel">{children}</span>
  return (
    <span
      className="spoiler"
      role="button"
      tabIndex={0}
      aria-label="Spoiler: clique para mostrar"
      title="Spoiler: clique para mostrar"
      onClick={(e) => {
        e.stopPropagation()
        setVisivel(true)
      }}
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), setVisivel(true))}
    >
      <span aria-hidden="true">{children}</span>
    </span>
  )
}

function quebras(texto, chave) {
  const linhas = texto.split('\n')
  return (
    <Fragment key={`q${chave}`}>
      {linhas.map((l, i) => (
        <Fragment key={i}>
          {i > 0 && <br />}
          {l}
        </Fragment>
      ))}
    </Fragment>
  )
}

function mencao(t, { membros, cargos, eu, aoMencionar }, chave) {
  if (t.startsWith('<@&')) {
    const id = Number(t.slice(3, -1))
    const c = cargos?.find((x) => x.id === id)
    const cor = c?.cor ? `#${c.cor.toString(16).padStart(6, '0')}` : undefined
    return (
      <span key={chave} className="mencao" style={cor ? { '--cor-mencao': cor } : undefined}>
        @{c?.nome || 'cargo apagado'}
      </span>
    )
  }
  const id = Number(t.slice(2, -1))
  const m = membros?.find((x) => x.usuarioId === id)
  const nome = m ? m.apelido || m.nomeExibicao : id === eu?.id ? eu.nomeExibicao : 'alguém'
  return (
    <button key={chave} type="button" className={`mencao ${id === eu?.id ? 'mencao-eu' : ''}`} onClick={() => aoMencionar?.(id)}>
      @{nome}
    </button>
  )
}

/** Troca "@Nome" (escolhido no autocompletar) pelo formato <@id> antes de enviar. */
export function prepararTexto(texto, escolhidos) {
  let saida = texto
  for (const { rotulo, codigo } of escolhidos) saida = saida.split(rotulo).join(codigo)
  return saida
}
