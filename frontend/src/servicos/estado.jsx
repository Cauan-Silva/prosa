// Estado do aplicativo: servidores, canais, mensagens, presença e quem está digitando.
// Tudo chega pelo gateway (WebSocket) e pelas chamadas à API, e passa por um reducer só.
import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef } from 'react'
import { api, enderecoGateway } from './api.js'
import { salaDeVoz, tocar } from './sons.js'
import { carregarDaConta, preferencias } from './preferencias.js'
import { verificarVersao } from './versao.js'

const Contexto = createContext(null)

const inicial = {
  conexao: 'conectando', // conectando | online | reconectando
  pronto: false,
  eu: null,
  servidores: {}, // id -> visão (servidor, cargos, categorias, canais, eu, totalMembros)
  online: {}, // servidorId -> { usuarioId: true }
  membros: {}, // servidorId -> [membro]
  mensagens: {}, // canalId -> { lista, inicio (não há mais antigas), carregando }
  digitando: {}, // canalId -> { usuarioId: { nome, ate } }
  voz: {}, // reservado para a etapa da voz
}

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
      return { ...estado, membros, eu }
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
        mencoesNaoLidas: !minha && (m.mencoes?.includes(estado.eu?.id) || m.mencionaTodos) ? (c.mencoesNaoLidas || 0) + 1 : c.mencoesNaoLidas,
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
  'PERMISSOES_ALTERADAS',
])

export function EstadoProvider({ children }) {
  const [estado, despachar] = useReducer(reducer, inicial)
  const estadoRef = useRef(estado)
  estadoRef.current = estado
  const wsRef = useRef(null)
  const ouvintes = useRef(new Set())

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
        const acao = { tipo: e.t, s: e.s, c: e.c, u: e.u, d: e.d }
        if (e.t === 'PRONTO') {
          despachar(acao)
          carregarDaConta(e.d.usuario?.preferencias)
          // reconectou (muitas vezes porque o servidor foi atualizado): confere se o site mudou
          setTimeout(verificarVersao, 3000)
          // Depois de reconectar, busca o que chegou nos canais abertos enquanto estava fora.
          for (const [canal, dados] of Object.entries(estadoRef.current.mensagens)) {
            const ultima = dados.lista.filter((x) => !x.pendente).at(-1)
            if (ultima) api.mensagens(canal, { depois: ultima.id }).then((lista) => despachar({ tipo: 'msgs', canal: Number(canal), modo: 'depois', lista })).catch(() => {})
          }
        } else if (estruturais.has(e.t)) {
          recarregar(e.s)
        } else {
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
  }, [recarregar])

  const mandar = useCallback((t, d) => {
    const ws = wsRef.current
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t, d }))
  }, [])

  const ouvir = useCallback((f) => {
    ouvintes.current.add(f)
    return () => ouvintes.current.delete(f)
  }, [])

  const valor = useMemo(() => ({ estado, despachar, mandar, ouvir, recarregar }), [estado, mandar, ouvir, recarregar])
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
    const mencao = m.mencoes?.includes(eu) || m.mencionaTodos
    notificar(e, m, mencao, estado)
    if (mencao) return tocar('mencao')
    const canalAberto = Number(location.pathname.match(/^\/s\/\d+\/(\d+)/)?.[1])
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
  try {
    const n = new Notification(`${membro?.apelido || m.autor.nomeExibicao}${canal ? ` em #${canal.nome}` : ''}`, {
      body: (m.conteudo || 'Enviou um anexo').replace(/<@&?\d+>/g, '@…').slice(0, 180),
      icon: membro?.avatar || '/icone-192.png',
      tag: `canal-${e.c}`,
    })
    n.onclick = () => {
      window.focus()
      history.pushState(null, '', `/s/${e.s}/${e.c}`)
      window.dispatchEvent(new Event('rota'))
      n.close()
    }
  } catch {
    // alguns navegadores só deixam notificar pelo service worker
  }
}
