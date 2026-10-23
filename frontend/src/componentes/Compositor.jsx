import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import { Avatar } from './Azulejo.jsx'
import SeletorEmoji from './SeletorEmoji.jsx'
import { CriarEnquete } from './Enquete.jsx'
import { emojisDisponiveis, nomeDe, useEstado } from '../servicos/estado.jsx'
import { api, enviarArquivo } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { prepararTexto } from '../servicos/texto.jsx'
import { tamanho } from '../servicos/formato.js'
import { tocar } from '../servicos/sons.js'

let contador = 0
const novoNonce = () => `${Date.now().toString(36)}${(contador++).toString(36)}${Math.random().toString(36).slice(2, 6)}`

export default function Compositor({ visao, canal, membros, podeEnviar, respondendo, aoCancelarResposta, aoEditarUltima, aoEnviar }) {
  const { estado, despachar, mandar } = useEstado()
  const [texto, setTexto] = useState('')
  const [escolhidos, setEscolhidos] = useState([]) // menções escolhidas no autocompletar
  const [envios, setEnvios] = useState([]) // anexos: { chave, nome, tamanho, progresso, anexo, erro }
  const [sugestao, setSugestao] = useState(null) // { tipo: mencao | emoji, inicio, busca, indice }
  const [seletor, setSeletor] = useState(false)
  const [enquete, setEnquete] = useState(false)
  const [arrastando, setArrastando] = useState(false)
  const [erro, setErro] = useState('')
  const campo = useRef(null)
  const arquivo = useRef(null)
  const ultimoDigitando = useRef(0)
  const cursor = useRef(null) // posição do cursor depois de escolher uma menção
  const perm = BigInt(canal.permissoes || 0)
  const podeAnexar = tem(perm, P.AnexarArquivos)
  const castigo = visao.eu.castigadoAte && new Date(visao.eu.castigadoAte) > new Date()

  useEffect(() => {
    if (respondendo) campo.current?.focus()
  }, [respondendo])

  useLayoutEffect(() => {
    if (cursor.current === null) return
    campo.current?.setSelectionRange(cursor.current, cursor.current)
    cursor.current = null
  }, [texto])

  // Altura automática do campo
  useEffect(() => {
    const el = campo.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 240)}px`
  }, [texto])

  const opcoes = useMemo(() => {
    if (!sugestao) return []
    const b = sugestao.busca.toLowerCase()
    if (sugestao.tipo === 'emoji') {
      return emojisDisponiveis(estado, visao.servidor.id)
        .filter((e) => e.nome.toLowerCase().includes(b))
        .sort((x, y) => x.nome.toLowerCase().startsWith(b) ? -1 : y.nome.toLowerCase().startsWith(b) ? 1 : 0)
        .slice(0, 8)
        .map((e) => ({ chave: `e${e.id}`, rotulo: e.nome, detalhe: e.servidor, codigo: `<${e.animado ? 'a' : ''}:${e.nome}:${e.id}>`, imagem: e.url, emoji: true }))
    }
    const pessoas = (membros || [])
      .filter((m) => [m.apelido, m.nomeExibicao, m.nomeUsuario].some((n) => n?.toLowerCase().includes(b)))
      .slice(0, 8)
      .map((m) => ({ chave: `u${m.usuarioId}`, rotulo: nomeDe(m), detalhe: `@${m.nomeUsuario}`, codigo: `<@${m.usuarioId}>`, id: m.usuarioId, avatar: m.avatar }))
    const podeTodos = tem(perm, P.MencionarTodos)
    const cargos = visao.cargos
      .filter((c) => !c.padrao && (c.mencionavel || podeTodos) && c.nome.toLowerCase().includes(b))
      .slice(0, 4)
      .map((c) => ({ chave: `c${c.id}`, rotulo: c.nome, detalhe: 'cargo', codigo: `<@&${c.id}>` }))
    const todos = podeTodos && 'todos'.includes(b) ? [{ chave: 'todos', rotulo: 'todos', detalhe: 'avisa todo mundo no canal', codigo: '@todos' }] : []
    return [...pessoas, ...cargos, ...todos]
  }, [sugestao, membros, visao.cargos, visao.servidor.id, perm, estado])

  function aoMudar(e) {
    const valor = e.target.value
    setTexto(valor)
    setErro('')
    const pos = e.target.selectionStart
    const antes = valor.slice(0, pos)
    const m = antes.match(/(?:^|\s)@([\p{L}\p{N}_.]{0,32})$/u)
    const em = antes.match(/(?:^|\s):([A-Za-z0-9_]{2,32})$/)
    setSugestao(
      m ? { tipo: 'mencao', inicio: pos - m[1].length - 1, busca: m[1], indice: 0 }
        : em ? { tipo: 'emoji', inicio: pos - em[1].length - 1, busca: em[1], indice: 0 }
          : null,
    )
    const agora = Date.now()
    if (valor.trim() && agora - ultimoDigitando.current > 3000) {
      ultimoDigitando.current = agora
      mandar('DIGITANDO', { canalId: canal.id })
    }
  }

  // insere um texto na posição do cursor (emoji do seletor)
  function inserir(trecho, escolha) {
    const el = campo.current
    const ini = el?.selectionStart ?? texto.length
    const fim = el?.selectionEnd ?? texto.length
    const antes = texto.slice(0, ini)
    const espaco = antes && !/\s$/.test(antes) && escolha ? ' ' : ''
    setTexto(antes + espaco + trecho + texto.slice(fim))
    if (escolha) setEscolhidos((l) => [...l.filter((x) => x.rotulo !== escolha.rotulo), escolha])
    cursor.current = ini + espaco.length + trecho.length
    el?.focus()
  }

  function escolher(op) {
    const el = campo.current
    const fim = el.selectionStart
    if (op.emoji) {
      const rotulo = `:${op.rotulo}:`
      setTexto(texto.slice(0, sugestao.inicio) + rotulo + ' ' + texto.slice(fim))
      setEscolhidos((l) => [...l.filter((x) => x.rotulo !== rotulo), { rotulo, codigo: op.codigo }])
      cursor.current = sugestao.inicio + rotulo.length + 1
      setSugestao(null)
      el.focus()
      return
    }
    const rotulo = op.codigo === '@todos' ? '@todos' : `@${op.rotulo}`
    const novo = texto.slice(0, sugestao.inicio) + rotulo + ' ' + texto.slice(fim)
    setTexto(novo)
    if (op.codigo !== '@todos') setEscolhidos((l) => [...l.filter((x) => x.rotulo !== rotulo), { rotulo, codigo: op.codigo }])
    cursor.current = sugestao.inicio + rotulo.length + 1
    setSugestao(null)
    el.focus()
  }

  function adicionarArquivos(lista) {
    if (!podeAnexar) {
      setErro('Você não pode anexar arquivos neste canal.')
      return
    }
    for (const f of [...lista].slice(0, 10 - envios.length)) {
      const chave = novoNonce()
      setEnvios((e) => [...e, { chave, nome: f.name, tamanho: f.size, progresso: 0 }])
      enviarArquivo(canal.id, f, (p) => setEnvios((e) => e.map((x) => (x.chave === chave ? { ...x, progresso: p } : x))))
        .then((anexo) => setEnvios((e) => e.map((x) => (x.chave === chave ? { ...x, progresso: 1, anexo } : x))))
        .catch((err) => setEnvios((e) => e.map((x) => (x.chave === chave ? { ...x, erro: err.message } : x))))
    }
  }

  async function enviar() {
    const conteudo = prepararTexto(texto.trim(), escolhidos)
    const prontos = envios.filter((e) => e.anexo)
    if (envios.some((e) => !e.anexo && !e.erro)) {
      setErro('Espere os arquivos terminarem de subir.')
      return
    }
    if (!conteudo && prontos.length === 0) return
    const nonce = novoNonce()
    const pendente = {
      id: 1e15 + contador++,
      nonce,
      pendente: true,
      autor: { id: estado.eu.id, nomeUsuario: estado.eu.nomeUsuario, nomeExibicao: estado.eu.nomeExibicao },
      conteudo,
      criadaEm: new Date().toISOString(),
      respondeA: respondendo ? { id: respondendo.id, autorNome: nomeDe(respondendo.autor), trecho: respondendo.conteudo.slice(0, 120) } : null,
      mencoes: [],
      reacoes: [],
      anexos: [],
    }
    despachar({ tipo: 'pendente', canal: canal.id, msg: pendente })
    tocar('enviar')
    setTexto('')
    setEscolhidos([])
    setEnvios([])
    setSugestao(null)
    ultimoDigitando.current = 0
    aoEnviar()
    try {
      const r = await api.enviar(canal.id, {
        conteudo,
        respondeA: respondendo?.id,
        anexos: prontos.map((e) => e.anexo.id),
        nonce,
      })
      despachar({ tipo: 'MENSAGEM_CRIADA', s: visao.servidor.id, c: canal.id, d: r })
    } catch (e) {
      despachar({ tipo: 'falhou', canal: canal.id, nonce, erro: e.message })
      tocar('erro')
    }
  }

  function teclas(e) {
    if (sugestao && opcoes.length) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        const d = e.key === 'ArrowDown' ? 1 : -1
        setSugestao({ ...sugestao, indice: (sugestao.indice + d + opcoes.length) % opcoes.length })
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        escolher(opcoes[sugestao.indice])
        return
      }
      if (e.key === 'Escape') {
        setSugestao(null)
        return
      }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      enviar()
    } else if (e.key === 'ArrowUp' && !texto) {
      e.preventDefault()
      aoEditarUltima()
    } else if (e.key === 'Escape' && respondendo) {
      aoCancelarResposta()
    }
  }

  if (!podeEnviar) {
    return (
      <div className="compositor bloqueado">
        {castigo
          ? `Você está de castigo até ${new Date(visao.eu.castigadoAte).toLocaleString('pt-BR')}. Dá para ler, mas não para escrever.`
          : canal.tipoConversa === 'dm'
            ? 'Não dá para mandar mensagens nesta conversa (alguém bloqueou).'
            : 'Você não pode enviar mensagens neste canal.'}
      </div>
    )
  }

  return (
    <div
      className={`compositor ${arrastando ? 'arrastando' : ''}`}
      onDragOver={(e) => {
        if (podeAnexar && e.dataTransfer.types.includes('Files')) {
          e.preventDefault()
          setArrastando(true)
        }
      }}
      onDragLeave={() => setArrastando(false)}
      onDrop={(e) => {
        e.preventDefault()
        setArrastando(false)
        adicionarArquivos(e.dataTransfer.files)
      }}
    >
      {sugestao && opcoes.length > 0 && (
        <ul className="sugestoes" role="listbox" aria-label={sugestao.tipo === 'emoji' ? 'Emojis' : 'Mencionar'}>
          {opcoes.map((op, i) => (
            <li key={op.chave} role="option" aria-selected={i === sugestao.indice}>
              <button onMouseDown={(e) => (e.preventDefault(), escolher(op))}>
                {op.imagem ? (
                  <img className="sugestao-emoji" src={op.imagem} alt="" />
                ) : op.id ? (
                  <Avatar nome={op.rotulo} id={op.id} src={op.avatar} tamanho={24} />
                ) : (
                  <span className="sugestao-arroba">@</span>
                )}
                <strong>{op.emoji ? `:${op.rotulo}:` : op.rotulo}</strong>
                <small>{op.detalhe}</small>
              </button>
            </li>
          ))}
        </ul>
      )}
      {respondendo && (
        <div className="respondendo">
          Respondendo a <strong>{nomeDe(respondendo.autor)}</strong>
          <button className="icone-botao" onClick={aoCancelarResposta} aria-label="Cancelar resposta">
            <Icone nome="fechar" tamanho={16} />
          </button>
        </div>
      )}
      {envios.length > 0 && (
        <ul className="envios">
          {envios.map((e) => (
            <li key={e.chave} className={e.erro ? 'erro' : ''}>
              <Icone nome="anexo" tamanho={16} />
              <span className="envio-nome">{e.nome}</span>
              <small>{e.erro || (e.anexo ? tamanho(e.tamanho) : `${Math.round(e.progresso * 100)}%`)}</small>
              <progress value={e.progresso} max={1} hidden={!!e.anexo || !!e.erro} />
              <button className="icone-botao" onClick={() => setEnvios((l) => l.filter((x) => x.chave !== e.chave))} aria-label={`Tirar ${e.nome}`}>
                <Icone nome="fechar" tamanho={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="compositor-linha">
        {podeAnexar && (
          <>
            <button className="icone-botao" onClick={() => arquivo.current.click()} aria-label="Anexar arquivo" title="Anexar arquivo">
              <Icone nome="anexo" />
            </button>
            <input ref={arquivo} type="file" multiple hidden onChange={(e) => (adicionarArquivos(e.target.files), (e.target.value = ''))} />
          </>
        )}
        <button className="icone-botao" onClick={() => setEnquete(true)} aria-label="Criar enquete" title="Criar enquete">
          <Icone nome="enquete" />
        </button>
        <textarea
          ref={campo}
          value={texto}
          onChange={aoMudar}
          onKeyDown={teclas}
          onPaste={(e) => e.clipboardData.files.length && (e.preventDefault(), adicionarArquivos(e.clipboardData.files))}
          placeholder={canal.ehFio ? 'Responder no fio' : canal.tipoConversa === 'dm' ? `Mensagem para @${canal.outros?.[0]?.nomeUsuario || canal.nome}` : canal.tipoConversa ? `Mensagem em ${canal.nome}` : `Mensagem em #${canal.nome}`}
          rows={1}
          maxLength={4000}
          aria-label={`Mensagem em #${canal.nome}`}
        />
        <span className="compositor-emoji">
          <button className={`icone-botao ${seletor ? 'ligado' : ''}`} onClick={() => setSeletor((s) => !s)} aria-label="Emojis" title="Emojis" aria-expanded={seletor}>
            <Icone nome="emoji" />
          </button>
          {seletor && (
            <SeletorEmoji
              servidorId={visao.servidor.id}
              aoFechar={() => setSeletor(false)}
              aoEscolher={({ texto: t, emoji }) => {
                if (t) inserir(t)
                else inserir(`:${emoji.nome}: `, { rotulo: `:${emoji.nome}:`, codigo: `<${emoji.animado ? 'a' : ''}:${emoji.nome}:${emoji.id}>` })
                setSeletor(false)
              }}
            />
          )}
        </span>
        <button className="icone-botao enviar" onClick={enviar} aria-label="Enviar" disabled={!texto.trim() && !envios.some((e) => e.anexo)}>
          <Icone nome="enviar" />
        </button>
      </div>
      {erro && <p className="erro compositor-erro">{erro}</p>}
      {enquete && (
        <CriarEnquete
          canal={canal}
          aoFechar={() => setEnquete(false)}
          aoCriar={(m) => {
            despachar({ tipo: 'MENSAGEM_CRIADA', s: visao.servidor.id, c: canal.id, d: m })
            aoEnviar()
          }}
        />
      )}
    </div>
  )
}
