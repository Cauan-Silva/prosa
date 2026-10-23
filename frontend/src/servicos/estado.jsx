// Estado do aplicativo: servidores, canais, mensagens, presença e quem está digitando.
// Tudo chega pelo gateway (WebSocket) e pelas chamadas à API, e passa por um reducer só.
import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef } from 'react'
import { api, enderecoGateway } from './api.js'
import { salaDeVoz, tocar } from './sons.js'
import { carregarDaConta, preferencias } from './preferencias.js'
import { verificarVersao } from './versao.js'

const Contexto = createContext(null)

/** Fio aberto no painel lateral (sem som de mensagem nova para ele). */
export const painelFio = { id: null }

const inicial = {
  conexao: 'conectando', // conectando | online | reconectando
  pronto: false,
  eu: null,
  servidores: {}, // id -> visão (servidor, cargos, categorias, canais, eu, totalMembros)
  online: {}, // servidorId -> { usuarioId: true }
  membros: {}, // servidorId -> [membro]
  mensagens: {}, // canalId -> { lista, inicio (não há mais antigas), carregando }
  digitando: {}, // canalId -> { usuarioId: { nome, ate } }
  voz: {}, // servidorId -> { canalId: [participantes] }
  // mensagens diretas e amigos (não pertencem a nenhum servidor)
  conversas: null, // [conversa] (null = ainda não carregou)
  amigos: null, // { amigos, recebidos, enviados, bloqueados }
}

/** Uma conversa direta vista como um canal de texto (para reaproveitar a tela de mensagens). */
export function canalDaConversa(c, eu) {
  const outros = c.participantes.filter((p) => p.id !== eu)
  const nome = c.tipo === 'dm' ? outros[0]?.nomeExibicao || 'Conversa' : c.nome || outros.map((p) => p.nomeExibicao).join(', ') || 'Grupo vazio'
  return { ...c, tipoConversa: c.tipo, tipo: 'texto', nome, topico: '', outros }
}

/** Participantes no formato de membro (nome, foto, status) para menções e mensagens. */
export const membrosDaConversa = (c) =>
  c.participantes.map((p) => ({
    usuarioId: p.id, nomeUsuario: p.nomeUsuario, nomeExibicao: p.nomeExibicao, avatar: p.avatar,
    status: p.status, statusTexto: p.statusTexto, statusEmoji: p.statusEmoji, cargos: [],
  }))

const porId = (lista) => Object.fromEntries(lista.map((x) => [x.id, x]))

function ordenarMsgs(lista) {
  return lista.sort((a, b) => (a.pendente ? 1 : 0) - (b.pendente ? 1 : 0) || a.id - b.id)
}

function comCanal(estado, canalId, fn) {
  const atual = estado.mensagens[canalId]
  if (!atual) return estado
  return { ...estado, mensagens: { ...estado.mensagens, [canalId]: fn(atual) } }
}

function alterarCanalDaVisao(estado, servidorId, canalId, fn) {
  if (!servidorId) {
    // conversa direta
    if (!estado.conversas?.some((c) => c.id === canalId)) return estado
    return { ...estado, conversas: estado.conversas.map((c) => (c.id === canalId ? fn(c) : c)) }
  }
  const v = estado.servidores[servidorId]
  if (!v) return estado
  return {
    ...estado,
    servidores: { ...estado.servidores, [servidorId]: { ...v, canais: v.canais.map((c) => (c.id === canalId ? fn(c) : c)) } },
  }
}

function reducer(estado, a) {
  switch (a.tipo) {
    case 'conexao':
      return { ...estado, conexao: a.valor }

    case 'PRONTO': {
      const online = {}
      for (const [s, ids] of Object.entries(a.d.online || {})) online[s] = Object.fromEntries(ids.map((u) => [u, true]))
      return {
        ...estado,
        pronto: true,
        conexao: 'online',
        eu: a.d.usuario,
        servidores: Object.fromEntries(a.d.servidores.map((v) => [v.servidor.id, v])),
        online,
        voz: a.d.extras?.voz || {},
      }
    }

    case 'visao': {
      // as menções não lidas só existem aqui no navegador: mantém ao recarregar a visão
      const antes = Object.fromEntries((estado.servidores[a.id]?.canais || []).map((c) => [c.id, c.mencoesNaoLidas]))
      const canais = a.visao.canais.map((c) => (antes[c.id] ? { ...c, mencoesNaoLidas: antes[c.id] } : c))
      return { ...estado, servidores: { ...estado.servidores, [a.id]: { ...a.visao, canais } } }
    }

    case 'eu':
      return { ...estado, eu: { ...estado.eu, ...a.usuario } }

    case 'conversas':
      // o servidor conta as não lidas; depois o navegador soma as que chegam
      return { ...estado, conversas: a.lista.map((c) => ({ ...c, mencoesNaoLidas: c.naoLidas })) }

    case 'conversa': // uma conversa nova ou atualizada (ao abrir uma DM)
      return {
        ...estado,
        conversas: [{ ...a.conversa, mencoesNaoLidas: a.conversa.naoLidas }, ...(estado.conversas || []).filter((c) => c.id !== a.conversa.id)],
      }

    case 'amigos': {
      const online = Object.fromEntries(a.lista.amigos.filter((p) => p.online).map((p) => [p.id, true]))
      return { ...estado, amigos: a.lista, online: { ...estado.online, 0: online } }
    }

    case 'USUARIO_ALTERADO': {
      const d = a.d
      const campos = {
        nomeExibicao: d.nomeExibicao, avatar: d.avatar, corPerfil: d.corPerfil,
        status: d.status, statusTexto: d.statusTexto, statusEmoji: d.statusEmoji,
      }
      const membros = {}
      for (const [sid, lista] of Object.entries(estado.membros)) {
        membros[sid] = lista.some((m) => m.usuarioId === d.usuarioId)
          ? lista.map((m) => (m.usuarioId === d.usuarioId ? { ...m, ...campos } : m))
          : lista
      }
      // a própria pessoa (em outra aba) recebe o status real, inclusive "invisível"
      const eu = d.usuarioId === estado.eu?.id && !a.s ? { ...estado.eu, ...d, status: estado.eu.status } : estado.eu
      const pessoa = (p) => (p.id === d.usuarioId ? { ...p, ...campos } : p)
      const conversas = estado.conversas?.map((c) =>
        c.participantes.some((p) => p.id === d.usuarioId) ? { ...c, participantes: c.participantes.map(pessoa) } : c,
      )
      const amigos = estado.amigos && Object.fromEntries(Object.entries(estado.amigos).map(([k, l]) => [k, l.map(pessoa)]))
      return { ...estado, membros, eu, conversas, amigos }
    }

    case 'SERVIDOR_CRIADO': {
      const online = Object.fromEntries((a.d.online || []).map((u) => [u, true]))
      return {
        ...estado,
        servidores: { ...estado.servidores, [a.s]: a.d.visao },
        online: { ...estado.online, [a.s]: online },
      }
    }

    case 'SERVIDOR_APAGADO': {
      const servidores = { ...estado.servidores }
      delete servidores[a.s]
      return { ...estado, servidores }
    }

    case 'PRESENCA': {
      const atual = { ...(estado.online[a.s] || {}) }
      if (a.d.online) atual[a.d.usuarioId] = true
      else delete atual[a.d.usuarioId]
      return { ...estado, online: { ...estado.online, [a.s]: atual } }
    }

    case 'membros':
      return { ...estado, membros: { ...estado.membros, [a.s]: a.lista } }

    case 'MEMBRO_ENTROU':
    case 'MEMBRO_ALTERADO': {
      const lista = estado.membros[a.s]
      const v = estado.servidores[a.s]
      let servidores = estado.servidores
      if (a.tipo === 'MEMBRO_ENTROU' && v) servidores = { ...servidores, [a.s]: { ...v, totalMembros: v.totalMembros + 1 } }
      if (!lista) return { ...estado, servidores }
      const outros = lista.filter((m) => m.usuarioId !== a.d.usuarioId)
      return { ...estado, servidores, membros: { ...estado.membros, [a.s]: [...outros, a.d] } }
    }

    case 'MEMBRO_SAIU': {
      const v = estado.servidores[a.s]
      const servidores = v ? { ...estado.servidores, [a.s]: { ...v, totalMembros: Math.max(0, v.totalMembros - 1) } } : estado.servidores
      const lista = estado.membros[a.s]
      return {
        ...estado,
        servidores,
        membros: lista ? { ...estado.membros, [a.s]: lista.filter((m) => m.usuarioId !== a.u) } : estado.membros,
      }
    }

    case 'msgs': {
      // a.modo: 'inicio' (as mais recentes), 'antes' (mais antigas), 'depois' (o que perdeu)
      const atual = estado.mensagens[a.canal]
      const pendentes = atual ? atual.lista.filter((m) => m.pendente) : []
      let lista
      if (a.modo === 'inicio' || !atual) lista = [...a.lista, ...pendentes]
      else {
        const vistos = porId(atual.lista.filter((m) => !m.pendente))
        for (const m of a.lista) vistos[m.id] = m
        lista = [...Object.values(vistos), ...pendentes]
      }
      const inicio = a.modo === 'depois' ? (atual?.inicio ?? false) : a.lista.length < 50
      return { ...estado, mensagens: { ...estado.mensagens, [a.canal]: { lista: ordenarMsgs(lista), inicio } } }
    }

    case 'pendente':
      return comCanal(estado, a.canal, (c) => ({ ...c, lista: ordenarMsgs([...c.lista, a.msg]) }))

    case 'falhou':
      return comCanal(estado, a.canal, (c) => ({
        ...c,
        lista: c.lista.map((m) => (m.nonce === a.nonce ? { ...m, falhou: a.erro } : m)),
      }))

    case 'descartar':
      return comCanal(estado, a.canal, (c) => ({ ...c, lista: c.lista.filter((m) => m.nonce !== a.nonce) }))

    case 'MENSAGEM_CRIADA': {
      const m = a.d
      let novo = comCanal(estado, a.c, (c) => {
        const lista = c.lista.filter((x) => x.id !== m.id && !(m.nonce && x.nonce === m.nonce))
        return { ...c, lista: ordenarMsgs([...lista, { ...m, nonce: undefined }]) }
      })
      const digit = novo.digitando[a.c]
      if (digit && m.autor && digit[m.autor.id]) {
        const resto = { ...digit }
        delete resto[m.autor.id]
        novo = { ...novo, digitando: { ...novo.digitando, [a.c]: resto } }
      }
      const minha = m.autor?.id === estado.eu?.id
      return alterarCanalDaVisao(novo, a.s, a.c, (c) => ({
        ...c,
        ultimaMensagemId: Math.max(c.ultimaMensagemId || 0, m.id),
        ultimaLida: minha ? Math.max(c.ultimaLida || 0, m.id) : c.ultimaLida,
        // numa conversa direta, toda mensagem nova conta no selo (como uma menção)
        mencoesNaoLidas: !minha && (!a.s || m.mencoes?.includes(estado.eu?.id) || m.mencionaTodos) ? (c.mencoesNaoLidas || 0) + 1 : c.mencoesNaoLidas,
      }))
    }

    case 'FIO_ATUALIZADO': {
      // o resumo do fio fica na mensagem de origem, no canal pai (a.c)
      const f = a.d
      if (!f.mensagemId) return estado
      return comCanal(estado, a.c, (c) => ({
        ...c,
        lista: c.lista.map((m) => {
          if (m.id !== f.mensagemId) return m
          if (f.apagado) {
            const { fio: _, ...resto } = m
            return resto
          }
          return { ...m, fio: f }
        }),
      }))
    }

    case 'MENSAGEM_EDITADA':
      return comCanal(estado, a.c, (c) => ({ ...c, lista: c.lista.map((m) => (m.id === a.d.id ? a.d : m)) }))

    case 'MENSAGEM_APAGADA':
      return comCanal(estado, a.c, (c) => ({ ...c, lista: c.lista.filter((m) => m.id !== a.d.id) }))

    case 'REACAO_ADICIONADA':
    case 'REACAO_REMOVIDA': {
      const { mensagemId, usuarioId, emoji } = a.d
      return comCanal(estado, a.c, (c) => ({
        ...c,
        lista: c.lista.map((m) => {
          if (m.id !== mensagemId) return m
          let reacoes = m.reacoes.map((r) => ({ ...r, usuarios: [...r.usuarios] }))
          let r = reacoes.find((x) => x.emoji === emoji)
          if (a.tipo === 'REACAO_ADICIONADA') {
            if (!r) reacoes.push((r = { emoji, usuarios: [] }))
            if (!r.usuarios.includes(usuarioId)) r.usuarios.push(usuarioId)
          } else if (r) {
            r.usuarios = r.usuarios.filter((u) => u !== usuarioId)
            reacoes = reacoes.filter((x) => x.usuarios.length > 0)
          }
          return { ...m, reacoes }
        }),
      }))
    }

    case 'DIGITANDO': {
      if (a.d.usuarioId === estado.eu?.id) return estado
      const canal = { ...(estado.digitando[a.c] || {}), [a.d.usuarioId]: { nome: a.d.nome, ate: Date.now() + 6000 } }
      return { ...estado, digitando: { ...estado.digitando, [a.c]: canal } }
    }

    case 'limparDigitando': {
      const agora = Date.now()
      let mudou = false
      const digitando = {}
      for (const [canal, pessoas] of Object.entries(estado.digitando)) {
        const vivas = Object.fromEntries(Object.entries(pessoas).filter(([, p]) => p.ate > agora))
        if (Object.keys(vivas).length !== Object.keys(pessoas).length) mudou = true
        if (Object.keys(vivas).length) digitando[canal] = vivas
      }
      return mudou ? { ...estado, digitando } : estado
    }

    case 'lido':
      return alterarCanalDaVisao(estado, a.s, a.canal, (c) => ({
        ...c,
        ultimaLida: Math.max(c.ultimaLida || 0, a.id),
        mencoesNaoLidas: 0,
      }))

    case 'VOZ_ESTADO': {
      const salas = { ...(estado.voz[a.s] || {}) }
      if (a.d.participantes?.length) salas[a.d.canalId] = a.d.participantes
      else delete salas[a.d.canalId]
      return { ...estado, voz: { ...estado.voz, [a.s]: salas } }
    }

    default:
      return estado
  }
}

// Eventos que mudam a estrutura do servidor: a tela recarrega a visão (é uma consulta só).
const estruturais = new Set([
  'SERVIDOR_ALTERADO', 'CANAL_CRIADO', 'CANAL_ALTERADO', 'CANAL_APAGADO', 'CATEGORIA_CRIADA',
  'CATEGORIA_ALTERADA', 'CATEGORIA_APAGADA', 'CARGO_CRIADO', 'CARGO_ALTERADO', 'CARGO_APAGADO',
  'PERMISSOES_ALTERADAS', 'EMOJIS_ALTERADOS',
])

/** Todos os emojis dos servidores da pessoa (o do servidor aberto primeiro). */
export function emojisDisponiveis(estado, servidorId) {
  const lista = []
  const vistos = new Set()
  const visoes = Object.values(estado.servidores).sort((a, b) => (b.servidor.id === servidorId) - (a.servidor.id === servidorId))
  for (const v of visoes) {
    for (const e of v.emojis || []) {
      if (vistos.has(e.id)) continue
      vistos.add(e.id)
      lista.push({ ...e, servidor: v.servidor.nome, servidorId: v.servidor.id })
    }
  }
  return lista
}

export function EstadoProvider({ children }) {
  const [estado, despachar] = useReducer(reducer, inicial)
  const estadoRef = useRef(estado)
  estadoRef.current = estado
  const wsRef = useRef(null)
  const ouvintes = useRef(new Set())

  // Amigos e conversas diretas (agrupa eventos seguidos numa consulta só).
  const esperas = useRef({})
  const depois = useCallback((chave, fn) => {
    clearTimeout(esperas.current[chave])
    esperas.current[chave] = setTimeout(fn, 150)
  }, [])
  const carregarAmigos = useCallback(
    () => depois('amigos', () => api.amigos().then((lista) => despachar({ tipo: 'amigos', lista })).catch(() => {})),
    [depois],
  )
  const carregarConversas = useCallback(
    () => depois('conversas', () => api.conversas().then((lista) => despachar({ tipo: 'conversas', lista })).catch(() => {})),
    [depois],
  )

  // Recarrega a visão de um servidor (agrupa vários eventos seguidos numa consulta só).
  const recargas = useRef({})
  const recarregar = useCallback((id) => {
    clearTimeout(recargas.current[id])
    recargas.current[id] = setTimeout(() => {
      api
        .servidor(id)
        .then((visao) => despachar({ tipo: 'visao', id, visao }))
        .catch(() => {})
    }, 120)
  }, [])

  useEffect(() => {
    let tentativa = 0
    let fechado = false
    let espera

    function conectar() {
      const ws = new WebSocket(enderecoGateway())
      wsRef.current = ws
      let abriu = false
      ws.onopen = () => {
        abriu = true
        tentativa = 0
      }
      ws.onmessage = (m) => {
        let e
        try {
          e = JSON.parse(m.data)
        } catch {
          return
        }
        const acao = { tipo: e.t, s: e.s || 0, c: e.c, u: e.u, d: e.d }
        if (e.t === 'PRONTO') {
          despachar(acao)
          carregarDaConta(e.d.usuario?.preferencias)
          carregarAmigos()
          carregarConversas()
          // reconectou (muitas vezes porque o servidor foi atualizado): confere se o site mudou
          setTimeout(verificarVersao, 3000)
          // Depois de reconectar, busca o que chegou nos canais abertos enquanto estava fora.
          for (const [canal, dados] of Object.entries(estadoRef.current.mensagens)) {
            const ultima = dados.lista.filter((x) => !x.pendente).at(-1)
            if (ultima) api.mensagens(canal, { depois: ultima.id }).then((lista) => despachar({ tipo: 'msgs', canal: Number(canal), modo: 'depois', lista })).catch(() => {})
          }
        } else if (estruturais.has(e.t)) {
          recarregar(e.s)
        } else if (e.t === 'AMIGO_ALTERADO') {
          carregarAmigos()
        } else if (e.t === 'CONVERSA_ALTERADA') {
          carregarConversas()
        } else {
          // mensagem numa conversa que ainda não está na lista (DM nova ou que estava fechada)
          if (e.t === 'MENSAGEM_CRIADA' && !e.s && !estadoRef.current.conversas?.some((c) => c.id === e.c)) carregarConversas()
          if (e.t === 'MEMBRO_ALTERADO' && e.d?.usuarioId === estadoRef.current.eu?.id) recarregar(e.s)
          despachar(acao)
        }
        sonsDoEvento(e, estadoRef.current)
        for (const f of ouvintes.current) f(e)
      }
      ws.onclose = (ev) => {
        if (fechado) return
        // Não abriu: pode ser o token vencido (a API avisa com "sessao-expirada").
        if (!abriu) api.eu().catch(() => {})
        despachar({ tipo: 'conexao', valor: 'reconectando' })
        const atraso = Math.min(15000, 500 * 2 ** tentativa) * (0.7 + Math.random() * 0.6)
        tentativa++
        espera = setTimeout(conectar, atraso)
      }
    }
    conectar()
    const limpar = setInterval(() => despachar({ tipo: 'limparDigitando' }), 2000)
    return () => {
      fechado = true
      clearTimeout(espera)
      clearInterval(limpar)
      wsRef.current?.close()
    }
  }, [recarregar, carregarAmigos, carregarConversas])

  const mandar = useCallback((t, d) => {
    const ws = wsRef.current
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t, d }))
  }, [])

  const ouvir = useCallback((f) => {
    ouvintes.current.add(f)
    return () => ouvintes.current.delete(f)
  }, [])

  const valor = useMemo(
    () => ({ estado, despachar, mandar, ouvir, recarregar, carregarAmigos, carregarConversas }),
    [estado, mandar, ouvir, recarregar, carregarAmigos, carregarConversas],
  )
  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>
}

export const useEstado = () => useContext(Contexto)

// Sons para o que chega dos outros: mensagens, menções e gente entrando/saindo da sua sala de voz.
function sonsDoEvento(e, estado) {
  const eu = estado.eu?.id
  if (e.t === 'MENSAGEM_CRIADA') {
    const m = e.d
    // status "não perturbe": sem sons nem notificações de mensagem
    if (!m?.autor || m.autor.id === eu || estado.eu?.status === 'ocupado') return
    // numa conversa direta toda mensagem conta como menção (é para você)
    const mencao = m.mencoes?.includes(eu) || m.mencionaTodos || !e.s
    notificar(e, m, mencao, estado)
    const canalAberto = Number(location.pathname.match(/^\/(?:s\/\d+|d)\/(\d+)/)?.[1])
    if (!e.s && !document.hidden && canalAberto === e.c) return
    if (!document.hidden && painelFio.id === e.c) return
    if (mencao) return tocar('mencao')
    if (document.hidden || canalAberto !== e.c) tocar('mensagem')
  } else if (e.t === 'VOZ_ESTADO' && e.d?.canalId === salaDeVoz.canalId) {
    const antes = new Set((estado.voz[e.s]?.[e.d.canalId] || []).map((p) => p.usuarioId))
    const depois = new Set((e.d.participantes || []).map((p) => p.usuarioId))
    const entrou = [...depois].some((id) => id !== eu && !antes.has(id))
    const saiu = [...antes].some((id) => id !== eu && !depois.has(id))
    if (entrou) tocar('entrou')
    else if (saiu) tocar('saiu')
  }
}

/** Nome que aparece para um membro (apelido, nome de exibição ou nome de usuário). */
export const nomeDe = (m) => m?.apelido || m?.nomeExibicao || m?.nomeUsuario || 'Alguém'

/** Cor do cargo mais alto que tem cor. */
export function corDe(membro, cargos) {
  if (!membro || !cargos) return null
  let melhor = null
  for (const c of cargos) {
    if (c.cor && membro.cargos?.includes(c.id) && (!melhor || c.posicao > melhor.posicao)) melhor = c
  }
  return melhor ? `#${melhor.cor.toString(16).padStart(6, '0')}` : null
}

// Notificação do sistema (Windows, macOS, Android) quando a aba está em segundo plano.
function notificar(e, m, mencao, estado) {
  const cfg = preferencias().notificacoes
  if (!cfg.area || !document.hidden || !('Notification' in window) || Notification.permission !== 'granted') return
  if (!mencao && !cfg.todas) return
  const v = estado.servidores[e.s]
  const canal = v?.canais.find((c) => c.id === e.c)
  const membro = estado.membros[e.s]?.find((x) => x.usuarioId === m.autor.id)
  const grupo = !e.s && estado.conversas?.find((c) => c.id === e.c && c.tipo === 'grupo')
  const onde = canal ? ` em #${canal.nome}` : grupo ? ` em ${grupo.nome || 'grupo'}` : ''
  try {
    const n = new Notification(`${membro?.apelido || m.autor.nomeExibicao}${onde}`, {
      body: (m.conteudo || 'Enviou um anexo').replace(/<@&?\d+>/g, '@…').slice(0, 180),
      icon: membro?.avatar || '/icone-192.png',
      tag: `canal-${e.c}`,
    })
    n.onclick = () => {
      window.focus()
      history.pushState(null, '', e.s ? `/s/${e.s}/${e.c}` : `/d/${e.c}`)
      window.dispatchEvent(new Event('rota'))
      n.close()
    }
  } catch {
    // alguns navegadores só deixam notificar pelo service worker
  }
}
