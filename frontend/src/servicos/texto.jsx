// Mostra o texto das mensagens: menções, links e um pouco de formatação
// (**negrito**, *itálico*, ~~riscado~~, `código` e blocos ```código```).
// Tudo vira elementos do React, nunca HTML cru, então não há como injetar código.
import { Fragment } from 'react'

const padrao =
  /(```[\s\S]+?```)|(`[^`\n]+`)|(<@&?\d{1,19}>)|(\B@todos\b)|(https?:\/\/[^\s<>"]+[^\s<>".,;:!?)\]'])|(\*\*[^*\n]+\*\*)|(~~[^~\n]+~~)|(\*[^*\n]+\*|_[^_\n]+_)/g

export function Texto({ conteudo, membros, cargos, eu, aoMencionar }) {
  return <>{partes(conteudo, { membros, cargos, eu, aoMencionar }, 0)}</>
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
    else if (m[5]) saida.push(<a key={k} href={t} target="_blank" rel="noopener noreferrer nofollow">{t}</a>)
    else if (nivel < 3 && m[6]) saida.push(<strong key={k}>{partes(t.slice(2, -2), ctx, nivel + 1)}</strong>)
    else if (nivel < 3 && m[7]) saida.push(<s key={k}>{partes(t.slice(2, -2), ctx, nivel + 1)}</s>)
    else if (nivel < 3 && m[8]) saida.push(<em key={k}>{partes(t.slice(1, -1), ctx, nivel + 1)}</em>)
    else saida.push(t)
    ultimo = m.index + t.length
  }
  if (ultimo < texto.length) saida.push(quebras(texto.slice(ultimo), i++))
  return saida
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
