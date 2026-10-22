import { Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import Mensagem from './Mensagem.jsx'
import Compositor from './Compositor.jsx'
import Busca from './Busca.jsx'
import SalaVoz from './SalaVoz.jsx'
import { useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { formatarDia } from '../servicos/formato.js'

const JUNTAR_MS = 7 * 60 * 1000

export default function Conversa({ visao, canal, mostrarMembros, aoAlternarMembros, aoVoltar }) {
  const [busca, setBusca] = useState(null) // termo buscado
  const [termo, setTermo] = useState('')

  return (
    <main className="conversa">
      <header className="conversa-topo">
        <button className="icone-botao so-celular" onClick={aoVoltar} aria-label="Voltar para os canais">
          <Icone nome="voltar" />
        </button>
        <Icone nome={canal.tipo === 'voz' ? 'voz' : 'hashtag'} tamanho={22} />
        <h1>{canal.nome}</h1>
        {canal.topico && <p className="topico" title={canal.topico}>{canal.topico}</p>}
        <form
          className="busca-campo"
          role="search"
          onSubmit={(e) => {
            e.preventDefault()
            if (termo.trim().length >= 2) setBusca(termo.trim())
          }}
        >
          <Icone nome="buscar" tamanho={16} />
          <input value={termo} onChange={(e) => setTermo(e.target.value)} placeholder={`Buscar em ${visao.servidor.nome}`} aria-label="Buscar mensagens" />
        </form>
        <button
          className={`icone-botao ${mostrarMembros ? 'ligado' : ''}`}
          onClick={aoAlternarMembros}
          aria-pressed={mostrarMembros}
          aria-label="Mostrar membros"
          title="Membros"
        >
          <Icone nome="pessoas" />
        </button>
      </header>

      <div className="conversa-corpo">
        {canal.tipo === 'voz' ? <SalaVoz visao={visao} canal={canal} /> : <CanalTexto visao={visao} canal={canal} />}
        {busca && <Busca visao={visao} termo={busca} aoFechar={() => setBusca(null)} />}
      </div>
    </main>
  )
}

function CanalTexto({ visao, canal }) {
  const { estado, despachar } = useEstado()
  const dados = estado.mensagens[canal.id]
  const lista = dados?.lista
  const membros = estado.membros[visao.servidor.id]
  const rolagem = useRef(null)
  const noFim = useRef(true)
  const alturaAntes = useRef(null)
  const [carregandoAntigas, setCarregandoAntigas] = useState(false)
  const [respondendo, setRespondendo] = useState(null)
  const [editando, setEditando] = useState(null)
  const [erro, setErro] = useState('')
  // Onde começam as mensagens novas (fixo enquanto o canal está aberto).
  const [marcoNovas] = useState(() => (canal.ultimaMensagemId > canal.ultimaLida ? canal.ultimaLida : null))
  const perm = BigInt(canal.permissoes || 0)
  const servidorId = visao.servidor.id

  useEffect(() => {
    if (dados) return
    api
      .mensagens(canal.id)
      .then((l) => despachar({ tipo: 'msgs', canal: canal.id, modo: 'inicio', lista: l }))
      .catch((e) => setErro(e.message))
  }, [canal.id, dados, despachar])

  const carregarAntigas = useCallback(async () => {
    if (!lista?.length || dados.inicio || carregandoAntigas) return
    setCarregandoAntigas(true)
    alturaAntes.current = rolagem.current.scrollHeight - rolagem.current.scrollTop
    try {
      const antigas = await api.mensagens(canal.id, { antes: lista.find((m) => !m.pendente)?.id })
      despachar({ tipo: 'msgs', canal: canal.id, modo: 'antes', lista: antigas })
    } catch (e) {
      setErro(e.message)
      alturaAntes.current = null
    } finally {
      setCarregandoAntigas(false)
    }
  }, [lista, dados, carregandoAntigas, canal.id, despachar])

  // Mantém a posição ao carregar as antigas; desce ao fim quando chega mensagem e já estava no fim.
  useLayoutEffect(() => {
    const el = rolagem.current
    if (!el) return
    if (alturaAntes.current !== null) {
      el.scrollTop = el.scrollHeight - alturaAntes.current
      alturaAntes.current = null
    } else if (noFim.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [lista])

  // Marca como lido quando a pessoa está vendo o fim do canal.
  const ultimaId = lista?.filter((m) => !m.pendente).at(-1)?.id || 0
  useEffect(() => {
    if (!ultimaId) return
    const marcar = () => {
      if (!noFim.current || document.visibilityState !== 'visible') return
      if (ultimaId <= (canal.ultimaLida || 0) && !canal.mencoesNaoLidas) return
      despachar({ tipo: 'lido', s: servidorId, canal: canal.id, id: ultimaId })
      api.marcarLido(canal.id, ultimaId).catch(() => {})
    }
    const t = setTimeout(marcar, 400)
    document.addEventListener('visibilitychange', marcar)
    return () => {
      clearTimeout(t)
      document.removeEventListener('visibilitychange', marcar)
    }
  }, [ultimaId, canal.id, canal.ultimaLida, canal.mencoesNaoLidas, servidorId, despachar])

  function rolou() {
    const el = rolagem.current
    noFim.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    if (el.scrollTop < 300) carregarAntigas()
  }

  const digitando = Object.values(estado.digitando[canal.id] || {})
  const minhaUltima = lista?.filter((m) => m.autor?.id === estado.eu.id && !m.pendente).at(-1)

  return (
    <div className="canal-texto">
      <div className="mensagens" ref={rolagem} onScroll={rolou} role="log" aria-live="polite" aria-label={`Mensagens de #${canal.nome}`}>
        {!lista && !erro && <p className="aviso-lista">Carregando mensagens…</p>}
        {erro && <p className="erro">{erro}</p>}
        {lista && dados.inicio && (
          <div className="inicio-canal">
            <h2>Começo de #{canal.nome}</h2>
            <p>{canal.topico || 'Este é o primeiro recado do canal. Diga oi!'}</p>
          </div>
        )}
        {carregandoAntigas && <p className="aviso-lista">Carregando mensagens antigas…</p>}
        {lista?.map((m, i) => {
          const anterior = lista[i - 1]
          const novoDia = !anterior || new Date(anterior.criadaEm).toDateString() !== new Date(m.criadaEm).toDateString()
          const marco = marcoNovas !== null && !m.pendente && m.id > marcoNovas && (!anterior || anterior.id <= marcoNovas)
          const junto =
            !novoDia && !marco && anterior && !m.respondeA && anterior.autor?.id === m.autor?.id &&
            new Date(m.criadaEm) - new Date(anterior.criadaEm) < JUNTAR_MS
          return (
            <Fragment key={m.nonce || m.id}>
              {novoDia && (
                <div className="separador-dia" role="separator">
                  <span>{formatarDia(m.criadaEm)}</span>
                </div>
              )}
              {marco && (
                <div className="separador-novas" role="separator">
                  <span>Novas</span>
                </div>
              )}
              <Mensagem
                m={m}
                junto={junto}
                visao={visao}
                canal={canal}
                membros={membros}
                editando={editando === m.id}
                aoEditar={setEditando}
                aoResponder={setRespondendo}
              />
            </Fragment>
          )
        })}
      </div>
      <div className="digitando" aria-live="polite">
        {digitando.length > 0 && (
          <>
            <span className="pontinhos" aria-hidden="true"><i /><i /><i /></span>
            {digitando.length === 1
              ? `${digitando[0].nome} está digitando…`
              : digitando.length < 4
                ? `${digitando.map((d) => d.nome).join(', ')} estão digitando…`
                : 'Várias pessoas estão digitando…'}
          </>
        )}
      </div>
      <Compositor
        visao={visao}
        canal={canal}
        membros={membros}
        podeEnviar={tem(perm, P.EnviarMensagens)}
        respondendo={respondendo}
        aoCancelarResposta={() => setRespondendo(null)}
        aoEditarUltima={() => minhaUltima && setEditando(minhaUltima.id)}
        aoEnviar={() => {
          noFim.current = true
          setRespondendo(null)
        }}
      />
    </div>
  )
}
