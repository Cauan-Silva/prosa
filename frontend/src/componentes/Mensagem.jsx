import { memo, useEffect, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import { Avatar } from './Azulejo.jsx'
import { Texto } from '../servicos/texto.jsx'
import { corDe, nomeDe, useEstado } from '../servicos/estado.jsx'
import { api, enderecoAnexo } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { formatarCompleto, formatarHora, quando, tamanho } from '../servicos/formato.js'
import { tocar } from '../servicos/sons.js'

/** Abre o cartão de perfil de alguém (o Aplicativo escuta este evento). */
export const abrirPerfil = (usuarioId) => window.dispatchEvent(new CustomEvent('prosa:perfil', { detail: { usuarioId } }))

export const RAPIDOS = ['👍', '❤️', '😂', '😮', '🎉', '🙏', '👀', '🔥']

function Mensagem({ m, junto, visao, canal, membros, editando, aoEditar, aoResponder }) {
  const { estado, despachar } = useEstado()
  const [seletor, setSeletor] = useState(false)
  const eu = estado.eu
  const perm = BigInt(canal.permissoes || 0)
  const minha = m.autor?.id === eu.id
  const membro = m.autor && membros?.find((x) => x.usuarioId === m.autor.id)
  const nome = m.autor ? nomeDe(membro || m.autor) : 'Conta apagada'
  const cor = corDe(membro, visao.cargos)
  const mencionaMe = m.mencoes?.includes(eu.id) || m.mencionaTodos

  async function reagir(emoji) {
    setSeletor(false)
    const r = m.reacoes.find((x) => x.emoji === emoji)
    const ja = r?.usuarios.includes(eu.id)
    try {
      if (ja) await api.desreagir(canal.id, m.id, emoji)
      else {
        tocar('reacao')
        await api.reagir(canal.id, m.id, emoji)
      }
    } catch (e) {
      alert(e.message)
    }
  }

  async function apagar(e) {
    if (!e.shiftKey && !confirm('Apagar esta mensagem? Não dá para desfazer.')) return
    try {
      await api.apagar(canal.id, m.id)
    } catch (err) {
      alert(err.message)
    }
  }

  async function reenviar() {
    despachar({ tipo: 'descartar', canal: canal.id, nonce: m.nonce })
    try {
      const r = await api.enviar(canal.id, { conteudo: m.conteudo, respondeA: m.respondeA?.id, nonce: m.nonce })
      despachar({ tipo: 'MENSAGEM_CRIADA', s: visao.servidor.id, c: canal.id, d: r })
    } catch (e) {
      despachar({ tipo: 'pendente', canal: canal.id, msg: { ...m, falhou: e.message } })
    }
  }

  const classes = ['mensagem', junto && 'junto', mencionaMe && 'me-menciona', m.pendente && 'pendente', m.falhou && 'falhou']
    .filter(Boolean)
    .join(' ')

  return (
    <article className={classes} id={`m${m.id}`} aria-label={`${nome}, ${quando(m.criadaEm)}`}>
      {m.respondeA && (
        <button
          className="resposta-a"
          onClick={() => document.getElementById(`m${m.respondeA.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })}
        >
          <Icone nome="responder" tamanho={14} />
          <strong>{m.respondeA.autorNome || 'Conta apagada'}</strong>
          <span>{m.respondeA.trecho || 'mensagem apagada'}</span>
        </button>
      )}
      <div className="mensagem-linha">
        {junto ? (
          <time className="hora-lateral" dateTime={m.criadaEm} title={formatarCompleto(m.criadaEm)}>
            {formatarHora(m.criadaEm)}
          </time>
        ) : (
          <button className="autor-foto" onClick={() => m.autor && abrirPerfil(m.autor.id)} aria-label={`Perfil de ${nome}`}>
            <Avatar nome={nome} id={m.autor?.id ?? 0} src={membro?.avatar} tamanho={40} />
          </button>
        )}
        <div className="mensagem-corpo">
          {!junto && (
            <header>
              <button className="autor-nome" style={cor ? { color: cor } : undefined} onClick={() => m.autor && abrirPerfil(m.autor.id)}>
                {nome}
              </button>
              <time dateTime={m.criadaEm} title={formatarCompleto(m.criadaEm)}>
                {quando(m.criadaEm)}
              </time>
            </header>
          )}
          {editando ? (
            <Edicao m={m} canal={canal} aoFechar={() => aoEditar(null)} />
          ) : (
            m.conteudo && (
              <div className="mensagem-texto">
                <Texto conteudo={m.conteudo} membros={membros} cargos={visao.cargos} eu={eu} />
                {m.editadaEm && (
                  <span className="editada" title={formatarCompleto(m.editadaEm)}>
                    (editada)
                  </span>
                )}
              </div>
            )
          )}
          {m.anexos?.length > 0 && (
            <div className="anexos">
              {m.anexos.map((a) => (
                <Anexo key={a.id} a={a} />
              ))}
            </div>
          )}
          {m.falhou && (
            <p className="falha-envio">
              Não foi enviada: {m.falhou}{' '}
              <button className="link" onClick={reenviar}>Tentar de novo</button>{' '}
              <button className="link" onClick={() => despachar({ tipo: 'descartar', canal: canal.id, nonce: m.nonce })}>Descartar</button>
            </p>
          )}
          {m.reacoes?.length > 0 && (
            <div className="reacoes">
              {m.reacoes.map((r) => {
                const minhaReacao = r.usuarios.includes(eu.id)
                const quem = r.usuarios
                  .slice(0, 5)
                  .map((u) => nomeDe(membros?.find((x) => x.usuarioId === u)) )
                  .join(', ')
                return (
                  <button
                    key={r.emoji}
                    className={`reacao ${minhaReacao ? 'minha' : ''}`}
                    onClick={() => reagir(r.emoji)}
                    aria-pressed={minhaReacao}
                    title={`${quem}${r.usuarios.length > 5 ? ` e mais ${r.usuarios.length - 5}` : ''}`}
                  >
                    <span>{r.emoji}</span> {r.usuarios.length}
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </div>

      {!m.pendente && !editando && (
        <div className="acoes" role="toolbar" aria-label="Ações da mensagem">
          {tem(perm, P.LerHistorico) && (
            <button className="icone-botao" onClick={() => setSeletor((s) => !s)} aria-label="Reagir" title="Reagir">
              <Icone nome="emoji" tamanho={18} />
            </button>
          )}
          {tem(perm, P.EnviarMensagens) && (
            <button className="icone-botao" onClick={() => aoResponder(m)} aria-label="Responder" title="Responder">
              <Icone nome="responder" tamanho={18} />
            </button>
          )}
          {minha && (
            <button className="icone-botao" onClick={() => aoEditar(m.id)} aria-label="Editar" title="Editar">
              <Icone nome="editar" tamanho={18} />
            </button>
          )}
          {(minha || tem(perm, P.GerenciarMensagens)) && (
            <button className="icone-botao perigo" onClick={apagar} aria-label="Apagar" title="Apagar (Shift+clique apaga sem perguntar)">
              <Icone nome="lixo" tamanho={18} />
            </button>
          )}
          {seletor && <Seletor aoEscolher={reagir} aoFechar={() => setSeletor(false)} />}
        </div>
      )}
    </article>
  )
}

export default memo(Mensagem)

function Seletor({ aoEscolher, aoFechar }) {
  const ref = useRef(null)
  const [outro, setOutro] = useState('')
  useEffect(() => {
    const fora = (e) => !ref.current?.contains(e.target) && aoFechar()
    document.addEventListener('pointerdown', fora)
    return () => document.removeEventListener('pointerdown', fora)
  }, [aoFechar])
  return (
    <div className="seletor-emoji" ref={ref}>
      {RAPIDOS.map((e) => (
        <button key={e} onClick={() => aoEscolher(e)} aria-label={`Reagir com ${e}`}>
          {e}
        </button>
      ))}
      <form
        onSubmit={(ev) => {
          ev.preventDefault()
          const e = outro.trim()
          if (e) aoEscolher([...e].slice(0, 2).join(''))
        }}
      >
        <input value={outro} onChange={(e) => setOutro(e.target.value)} placeholder="outro" aria-label="Outro emoji" maxLength={8} />
      </form>
    </div>
  )
}

function Edicao({ m, canal, aoFechar }) {
  const [texto, setTexto] = useState(m.conteudo)
  const [erro, setErro] = useState('')
  const ref = useRef(null)
  useEffect(() => {
    const el = ref.current
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [])
  async function salvar() {
    if (texto.trim() === m.conteudo) return aoFechar()
    try {
      await api.editar(canal.id, m.id, texto)
      aoFechar()
    } catch (e) {
      setErro(e.message)
    }
  }
  return (
    <div className="edicao">
      <textarea
        ref={ref}
        value={texto}
        rows={Math.min(8, texto.split('\n').length)}
        onChange={(e) => setTexto(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') aoFechar()
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            salvar()
          }
        }}
        aria-label="Editar mensagem"
      />
      <small>
        Esc para cancelar, Enter para salvar. {erro && <span className="erro">{erro}</span>}
      </small>
    </div>
  )
}

function Anexo({ a }) {
  const url = enderecoAnexo(a.endereco)
  if (a.tipo.startsWith('image/')) {
    return (
      <a href={url} target="_blank" rel="noopener noreferrer" className="anexo-imagem">
        <img src={url} alt={a.nome} loading="lazy" />
      </a>
    )
  }
  if (a.tipo.startsWith('video/')) return <video className="anexo-video" src={url} controls preload="metadata" />
  if (a.tipo.startsWith('audio/')) return <audio src={url} controls preload="none" />
  return (
    <a href={url} className="anexo-arquivo" target="_blank" rel="noopener noreferrer" download={a.nome}>
      <Icone nome="anexo" />
      <span>
        <strong>{a.nome}</strong>
        <small>{tamanho(a.tamanho)}</small>
      </span>
    </a>
  )
}
