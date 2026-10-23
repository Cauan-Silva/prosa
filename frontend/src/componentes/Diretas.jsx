// Início do Prosa: amigos e mensagens diretas (DMs e grupos), fora dos servidores.
import { useEffect, useMemo, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import Modal from './Modal.jsx'
import { Avatar } from './Azulejo.jsx'
import { abrirPerfil } from './Mensagem.jsx'
import { BarraVoz, Rodape } from './Canais.jsx'
import { CanalTexto } from './Conversa.jsx'
import { canalDaConversa, membrosDaConversa, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { navegar } from '../servicos/rota.jsx'

/** Abre (ou cria) a DM com alguém e vai para ela. */
export async function conversarCom(usuarioId, despachar) {
  const c = await api.criarConversa([usuarioId])
  despachar({ tipo: 'conversa', conversa: c })
  navegar(`/d/${c.id}`)
}

const naoLida = (c) => (c.ultimaMensagemId || 0) > (c.ultimaLida || 0)

// ---------- barra lateral ----------

export function BarraDiretas({ canalId, aoAbrir, aoEscolher }) {
  const { estado, despachar } = useEstado()
  const [nova, setNova] = useState(false)
  const conversas = estado.conversas || []
  const pendentes = estado.amigos?.recebidos.length || 0

  async function fechar(e, c) {
    e.stopPropagation()
    despachar({ tipo: 'conversas', lista: conversas.filter((x) => x.id !== c.id) })
    if (canalId === c.id) navegar('/')
    try {
      await api.fecharConversa(c.id)
    } catch {
      // volta na próxima recarga da lista
    }
  }

  return (
    <aside className="canais diretas" aria-label="Mensagens diretas">
      <div className="canais-topo diretas-topo">
        <button className="diretas-busca" onClick={() => setNova(true)}>
          Começar uma conversa
        </button>
      </div>
      <nav className="canais-lista">
        <button className={`conversa-item diretas-amigos ${!canalId ? 'ativo' : ''}`} onClick={() => aoEscolher(null)}>
          <Icone nome="pessoas" tamanho={20} />
          <span>Amigos</span>
          {pendentes > 0 && <span className="selo selo-inline">{pendentes}</span>}
        </button>
        <div className="grupo-titulo diretas-titulo">
          <span>Mensagens diretas</span>
          <button className="icone-botao" onClick={() => setNova(true)} title="Nova conversa ou grupo" aria-label="Nova conversa ou grupo">
            <Icone nome="mais" tamanho={16} />
          </button>
        </div>
        {estado.conversas === null && <p className="dica diretas-vazia">Carregando…</p>}
        {estado.conversas?.length === 0 && <p className="dica diretas-vazia">Nenhuma conversa ainda. Clique em alguém e escolha “Mensagem”.</p>}
        <ul>
          {conversas.map((c) => {
            const canal = canalDaConversa(c, estado.eu.id)
            const outro = canal.outros[0]
            const ativo = c.id === canalId
            return (
              <li key={c.id}>
                <div
                  role="button"
                  tabIndex={0}
                  className={`conversa-item ${ativo ? 'ativo' : ''} ${naoLida(c) && !ativo ? 'nao-lida' : ''}`}
                  onClick={() => aoEscolher(c.id)}
                  onKeyDown={(e) => e.key === 'Enter' && aoEscolher(c.id)}
                  aria-current={ativo ? 'page' : undefined}
                >
                  {c.tipo === 'dm' ? (
                    <Avatar nome={canal.nome} id={outro?.id} src={outro?.avatar} status={outro?.status} tamanho={32} online={!!estado.online[0]?.[outro?.id] || !!outro?.online} />
                  ) : (
                    <span className="avatar avatar-grupo" style={{ width: 32, height: 32 }}>
                      <Icone nome="pessoas" tamanho={18} />
                    </span>
                  )}
                  <span className="conversa-item-nome">
                    <span>{canal.nome}</span>
                    {c.tipo === 'grupo' && <small>{c.participantes.length} pessoas</small>}
                  </span>
                  {c.mencoesNaoLidas > 0 && !ativo && <span className="selo selo-inline">{c.mencoesNaoLidas > 99 ? '99+' : c.mencoesNaoLidas}</span>}
                  {c.tipo === 'dm' && (
                    <button className="icone-botao conversa-fechar" onClick={(e) => fechar(e, c)} title="Fechar conversa" aria-label={`Fechar conversa com ${canal.nome}`}>
                      <Icone nome="fechar" tamanho={14} />
                    </button>
                  )}
                </div>
              </li>
            )
          })}
        </ul>
      </nav>
      <BarraVoz />
      <Rodape aoAbrir={aoAbrir} />
      {nova && <NovaConversa aoFechar={() => setNova(false)} />}
    </aside>
  )
}

// ---------- escolher amigos (nova conversa, novo grupo ou adicionar ao grupo) ----------

export function NovaConversa({ aoFechar, grupo }) {
  const { estado, despachar } = useEstado()
  const [busca, setBusca] = useState('')
  const [escolhidos, setEscolhidos] = useState([])
  const [nome, setNome] = useState('')
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const jaNoGrupo = new Set(grupo?.participantes.map((p) => p.id) || [])
  const amigos = (estado.amigos?.amigos || []).filter((a) => !jaNoGrupo.has(a.id))
  const filtrados = amigos.filter((a) => `${a.nomeExibicao} ${a.nomeUsuario}`.toLowerCase().includes(busca.trim().toLowerCase()))
  const limite = grupo ? 10 - grupo.participantes.length : 9

  function alternar(id) {
    setEscolhidos((l) => (l.includes(id) ? l.filter((x) => x !== id) : l.length >= limite ? l : [...l, id]))
  }

  async function criar() {
    setErro('')
    setOcupado(true)
    try {
      if (grupo) {
        for (const id of escolhidos) await api.adicionarAoGrupo(grupo.id, id)
        aoFechar()
        return
      }
      const c = await api.criarConversa(escolhidos, escolhidos.length > 1 ? nome : '')
      despachar({ tipo: 'conversa', conversa: c })
      aoFechar()
      navegar(`/d/${c.id}`)
    } catch (e) {
      setErro(e.message)
    } finally {
      setOcupado(false)
    }
  }

  const rotulo = grupo ? 'Adicionar ao grupo' : escolhidos.length > 1 ? 'Criar grupo' : 'Abrir conversa'
  return (
    <Modal titulo={grupo ? 'Adicionar ao grupo' : 'Nova conversa'} aoFechar={aoFechar} largura="pequena">
      <div className="formulario nova-conversa">
        <p className="dica">
          {grupo ? `Escolha amigos para entrar (cabem mais ${limite}).` : 'Escolha um amigo para conversar, ou vários para criar um grupo (até 10 pessoas).'}
        </p>
        <input value={busca} onChange={(e) => setBusca(e.target.value)} placeholder="Buscar amigos" aria-label="Buscar amigos" autoFocus />
        {amigos.length === 0 ? (
          <p className="aviso-lista">
            {grupo ? 'Todos os seus amigos já estão no grupo.' : 'Você ainda não tem amigos no Prosa. Adicione alguém na aba “Amigos”.'}
          </p>
        ) : (
          <ul className="escolher-amigos">
            {filtrados.map((a) => (
              <li key={a.id}>
                <label>
                  <input type="checkbox" checked={escolhidos.includes(a.id)} onChange={() => alternar(a.id)} />
                  <Avatar nome={a.nomeExibicao} id={a.id} src={a.avatar} tamanho={28} />
                  <span>
                    {a.nomeExibicao} <small>@{a.nomeUsuario}</small>
                  </span>
                </label>
              </li>
            ))}
          </ul>
        )}
        {!grupo && escolhidos.length > 1 && (
          <label>
            Nome do grupo (opcional)
            <input value={nome} onChange={(e) => setNome(e.target.value)} maxLength={40} placeholder="Ex.: Trabalho de história" />
          </label>
        )}
        {erro && <p className="erro">{erro}</p>}
        <button className="botao primario" disabled={!escolhidos.length || ocupado} onClick={criar}>
          {ocupado ? 'Um instante…' : rotulo}
        </button>
      </div>
    </Modal>
  )
}

// ---------- amigos ----------

const ABAS = [
  ['online', 'Online'],
  ['todos', 'Todos'],
  ['pendentes', 'Pendentes'],
  ['bloqueados', 'Bloqueados'],
]

export function Amigos({ aoVoltar, aoCriarServidor, semServidores }) {
  const { estado, despachar, carregarAmigos } = useEstado()
  const [aba, setAba] = useState('online')
  const [msg, setMsg] = useState('')
  const lista = estado.amigos
  const online = estado.online[0] || {}
  const recebidos = lista?.recebidos.length || 0

  useEffect(() => setMsg(''), [aba])

  async function acao(fn, sucesso) {
    setMsg('')
    try {
      await fn()
      if (sucesso) setMsg(sucesso)
      carregarAmigos()
    } catch (e) {
      setMsg(e.message)
    }
  }

  let pessoas = []
  if (lista) {
    if (aba === 'online') pessoas = lista.amigos.filter((a) => online[a.id])
    else if (aba === 'todos') pessoas = lista.amigos
    else if (aba === 'pendentes') pessoas = [...lista.recebidos.map((p) => ({ ...p, recebido: true })), ...lista.enviados]
    else if (aba === 'bloqueados') pessoas = lista.bloqueados
  }

  return (
    <main className="conversa amigos">
      <header className="conversa-topo">
        <button className="icone-botao so-celular" onClick={aoVoltar} aria-label="Voltar para as conversas">
          <Icone nome="voltar" />
        </button>
        <Icone nome="pessoas" tamanho={22} />
        <h1>Amigos</h1>
        <nav className="abas-amigos" role="tablist">
          {ABAS.map(([id, nome]) => (
            <button key={id} role="tab" aria-selected={aba === id} onClick={() => setAba(id)}>
              {nome}
              {id === 'pendentes' && recebidos > 0 && <span className="selo selo-inline">{recebidos}</span>}
            </button>
          ))}
          <button role="tab" className="aba-adicionar" aria-selected={aba === 'adicionar'} onClick={() => setAba('adicionar')}>
            Adicionar amigo
          </button>
        </nav>
      </header>
      <div className="amigos-corpo">
        {semServidores && aba !== 'adicionar' && (
          <div className="amigos-convite-servidor">
            <img src="/capa-pequena.webp" alt="" width="72" height="72" />
            <div>
              <strong>Você ainda não está em nenhum servidor</strong>
              <p>Crie um para sua turma ou entre com o link de convite que alguém mandou.</p>
            </div>
            <button className="botao primario" onClick={aoCriarServidor}>
              Criar ou entrar
            </button>
          </div>
        )}
        {aba === 'adicionar' ? (
          <Adicionar />
        ) : (
          <>
            {msg && <p className="dica amigos-msg">{msg}</p>}
            {!lista && <p className="aviso-lista">Carregando…</p>}
            {lista && pessoas.length === 0 && (
              <div className="amigos-vazio">
                <p>
                  {aba === 'online'
                    ? 'Nenhum amigo online agora.'
                    : aba === 'todos'
                      ? 'Você ainda não adicionou ninguém.'
                      : aba === 'pendentes'
                        ? 'Nenhum pedido de amizade esperando.'
                        : 'Você não bloqueou ninguém.'}
                </p>
                {(aba === 'online' || aba === 'todos') && (
                  <button className="botao primario" onClick={() => setAba('adicionar')}>
                    Adicionar amigo
                  </button>
                )}
              </div>
            )}
            {pessoas.length > 0 && (
              <h2 className="amigos-contagem">
                {aba === 'online' ? `Online: ${pessoas.length}` : aba === 'todos' ? `Todos os amigos: ${pessoas.length}` : aba === 'pendentes' ? `Pendentes: ${pessoas.length}` : `Bloqueados: ${pessoas.length}`}
              </h2>
            )}
            <ul className="lista-amigos">
              {pessoas.map((p) => (
                <Amigo key={p.id} p={p} aba={aba} estaOnline={!!online[p.id]} acao={acao} despachar={despachar} />
              ))}
            </ul>
          </>
        )}
      </div>
    </main>
  )
}

function Amigo({ p, aba, estaOnline, acao, despachar }) {
  const [menu, setMenu] = useState(false)
  const ref = useRef(null)
  useEffect(() => {
    if (!menu) return
    const fora = (e) => !ref.current?.contains(e.target) && setMenu(false)
    document.addEventListener('pointerdown', fora)
    return () => document.removeEventListener('pointerdown', fora)
  }, [menu])
  const ehAmigo = aba === 'online' || aba === 'todos'
  const status = estaOnline ? (p.statusTexto ? `${p.statusEmoji || ''} ${p.statusTexto}`.trim() : rotuloStatus(p.status)) : 'Offline'

  return (
    <li className="amigo">
      <button className="amigo-pessoa" onClick={() => abrirPerfil(p.id)}>
        <Avatar nome={p.nomeExibicao} id={p.id} src={p.avatar} status={p.status} tamanho={36} online={ehAmigo ? estaOnline : undefined} />
        <span>
          <strong>{p.nomeExibicao}</strong>
          <small>
            @{p.nomeUsuario}
            {ehAmigo && ` · ${status}`}
            {aba === 'pendentes' && (p.recebido ? ' · quer ser seu amigo' : ' · pedido enviado')}
          </small>
        </span>
      </button>
      <div className="amigo-acoes" ref={ref}>
        {ehAmigo && (
          <>
            <button className="icone-botao" title="Mensagem" aria-label={`Mandar mensagem para ${p.nomeExibicao}`} onClick={() => acao(() => conversarCom(p.id, despachar))}>
              <Icone nome="mensagem" tamanho={18} />
            </button>
            <button className="icone-botao" title="Mais" aria-label={`Mais opções para ${p.nomeExibicao}`} aria-expanded={menu} onClick={() => setMenu((m) => !m)}>
              <Icone nome="pontos" tamanho={20} />
            </button>
            {menu && (
              <div className="menu">
                <button onClick={() => (setMenu(false), confirm(`Desfazer a amizade com ${p.nomeExibicao}?`) && acao(() => api.removerAmigo(p.id)))}>
                  Desfazer amizade
                </button>
                <button className="perigo" onClick={() => (setMenu(false), confirm(`Bloquear ${p.nomeExibicao}? Vocês deixam de ser amigos e ela não consegue mais mandar mensagem direta para você.`) && acao(() => api.bloquear(p.id)))}>
                  Bloquear
                </button>
              </div>
            )}
          </>
        )}
        {aba === 'pendentes' && p.recebido && (
          <>
            <button className="icone-botao aceitar" title="Aceitar" aria-label={`Aceitar o pedido de ${p.nomeExibicao}`} onClick={() => acao(() => api.pedirAmizade(p.id), `Agora você e ${p.nomeExibicao} são amigos.`)}>
              <Icone nome="check" tamanho={18} />
            </button>
            <button className="icone-botao perigo" title="Recusar" aria-label={`Recusar o pedido de ${p.nomeExibicao}`} onClick={() => acao(() => api.removerAmigo(p.id))}>
              <Icone nome="fechar" tamanho={18} />
            </button>
          </>
        )}
        {aba === 'pendentes' && !p.recebido && (
          <button className="icone-botao perigo" title="Cancelar pedido" aria-label={`Cancelar o pedido para ${p.nomeExibicao}`} onClick={() => acao(() => api.removerAmigo(p.id))}>
            <Icone nome="fechar" tamanho={18} />
          </button>
        )}
        {aba === 'bloqueados' && (
          <button className="botao" onClick={() => acao(() => api.desbloquear(p.id), `${p.nomeExibicao} foi desbloqueado(a).`)}>
            Desbloquear
          </button>
        )}
      </div>
    </li>
  )
}

const rotuloStatus = (s) => ({ ausente: 'Ausente', ocupado: 'Não perturbe' })[s] || 'Online'

function Adicionar() {
  const { carregarAmigos } = useEstado()
  const [nome, setNome] = useState('')
  const [msg, setMsg] = useState(null) // { ok, texto }
  const [ocupado, setOcupado] = useState(false)

  async function enviar(e) {
    e.preventDefault()
    setMsg(null)
    setOcupado(true)
    try {
      const r = await api.pedirAmizadePorNome(nome)
      setMsg({ ok: true, texto: r.resultado === 'amigos' ? `Pronto! Você e @${nome.replace(/^@/, '')} agora são amigos.` : `Pedido enviado para @${nome.replace(/^@/, '')}.` })
      setNome('')
      carregarAmigos()
    } catch (err) {
      setMsg({ ok: false, texto: err.message })
    } finally {
      setOcupado(false)
    }
  }

  return (
    <section className="adicionar-amigo">
      <h2>Adicionar amigo</h2>
      <p className="dica">Digite o nome de usuário da pessoa (o @nome que aparece no perfil dela).</p>
      <form className={`adicionar-campo ${msg ? (msg.ok ? 'ok' : 'erro') : ''}`} onSubmit={enviar}>
        <span aria-hidden="true">@</span>
        <input value={nome} onChange={(e) => setNome(e.target.value)} placeholder="nome de usuário" aria-label="Nome de usuário" maxLength={33} autoFocus />
        <button className="botao primario" disabled={!nome.trim() || ocupado}>
          Enviar pedido
        </button>
      </form>
      {msg && <p className={msg.ok ? 'sucesso' : 'erro'} role="status">{msg.texto}</p>}
    </section>
  )
}

// ---------- conversa ----------

export function ConversaDireta({ conversa, aoVoltar }) {
  const { estado } = useEstado()
  const [adicionar, setAdicionar] = useState(false)
  const [renomear, setRenomear] = useState(false)
  const eu = estado.eu.id
  const canal = useMemo(() => canalDaConversa(conversa, eu), [conversa, eu])
  const membros = useMemo(() => membrosDaConversa(conversa), [conversa])
  const visao = useMemo(
    () => ({ servidor: { id: 0, nome: 'Mensagens diretas' }, cargos: [], categorias: [], canais: [canal], eu: { permissoes: conversa.permissoes } }),
    [canal, conversa.permissoes],
  )
  const outro = canal.outros[0]
  const dono = conversa.donoId === eu

  async function sair() {
    if (!confirm(`Sair de “${canal.nome}”? Você só volta se alguém te adicionar de novo.`)) return
    try {
      await api.tirarDoGrupo(conversa.id, eu)
      navegar('/')
    } catch (e) {
      alert(e.message)
    }
  }

  return (
    <main className="conversa">
      <header className="conversa-topo">
        <button className="icone-botao so-celular" onClick={aoVoltar} aria-label="Voltar para as conversas">
          <Icone nome="voltar" />
        </button>
        {conversa.tipo === 'dm' ? (
          <button className="conversa-direta-titulo" onClick={() => outro && abrirPerfil(outro.id)}>
            <Avatar nome={canal.nome} id={outro?.id} src={outro?.avatar} status={outro?.status} tamanho={28} online={!!estado.online[0]?.[outro?.id] || !!outro?.online} />
            <h1>{canal.nome}</h1>
            {outro && <small>@{outro.nomeUsuario}</small>}
          </button>
        ) : (
          <div className="conversa-direta-titulo">
            <span className="avatar avatar-grupo" style={{ width: 28, height: 28 }}>
              <Icone nome="pessoas" tamanho={16} />
            </span>
            {renomear ? (
              <Renomear conversa={conversa} aoFechar={() => setRenomear(false)} />
            ) : (
              <h1>
                <button className="link-titulo" onClick={() => setRenomear(true)} title="Renomear o grupo">
                  {canal.nome}
                </button>
              </h1>
            )}
          </div>
        )}
        <span className="espaco" />
        {conversa.tipo === 'grupo' && (
          <>
            <div className="participantes-grupo" aria-label="Pessoas no grupo">
              {conversa.participantes.slice(0, 5).map((p) => (
                <button key={p.id} onClick={() => abrirPerfil(p.id)} title={p.nomeExibicao + (p.id === conversa.donoId ? ' (criou o grupo)' : '')}>
                  <Avatar nome={p.nomeExibicao} id={p.id} src={p.avatar} tamanho={24} />
                </button>
              ))}
              {conversa.participantes.length > 5 && <small>+{conversa.participantes.length - 5}</small>}
            </div>
            {conversa.participantes.length < 10 && (
              <button className="icone-botao" onClick={() => setAdicionar(true)} title="Adicionar amigos" aria-label="Adicionar amigos ao grupo">
                <Icone nome="pessoa-mais" />
              </button>
            )}
            <button className="icone-botao perigo" onClick={sair} title={dono ? 'Sair do grupo (passa para outra pessoa)' : 'Sair do grupo'} aria-label="Sair do grupo">
              <Icone nome="porta" />
            </button>
          </>
        )}
      </header>
      <div className="conversa-corpo">
        <CanalTexto visao={visao} canal={canal} membros={membros} />
      </div>
      {adicionar && <NovaConversa grupo={conversa} aoFechar={() => setAdicionar(false)} />}
    </main>
  )
}

function Renomear({ conversa, aoFechar }) {
  const [nome, setNome] = useState(conversa.nome)
  async function salvar(e) {
    e?.preventDefault()
    try {
      if (nome.trim() !== conversa.nome) await api.alterarConversa(conversa.id, { nome: nome.trim() })
    } catch (err) {
      alert(err.message)
    }
    aoFechar()
  }
  return (
    <form className="renomear-grupo" onSubmit={salvar}>
      <input value={nome} onChange={(e) => setNome(e.target.value)} onBlur={salvar} onKeyDown={(e) => e.key === 'Escape' && aoFechar()} maxLength={40} placeholder="Nome do grupo" aria-label="Nome do grupo" autoFocus />
    </form>
  )
}

