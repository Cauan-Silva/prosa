// Preferências da pessoa (aparência, voz, notificações). Ficam no navegador, para abrir
// já do jeito certo, e na conta, para valer em qualquer navegador em que ela entrar.
import { useSyncExternalStore } from 'react'
import { api } from './api.js'

const CHAVE = 'prosa.preferencias'

export const PADRAO = {
  aparencia: {
    tema: 'sistema', // id de TEMAS ou "sistema"
    destaque: '', // cor de destaque própria ("" = a do tema)
    fonte: 'schibsted',
    escala: 1,
    densidade: 'confortavel',
    cantos: 'arredondados',
    animacoes: true,
    sublinharLinks: false,
  },
  voz: {
    microfone: '', // deviceId ("" = padrão do sistema)
    saida: '',
    camera: '',
    supressaoRuido: true,
    cancelamentoEco: true,
    ganhoAutomatico: true,
    filtroRuido: false, // portão: corta o som abaixo da sensibilidade
    sensibilidade: -50, // dBFS
    modo: 'voz', // voz (detecção) | ptt (pressionar para falar)
    teclaPtt: 'KeyV',
    volumeSaida: 1,
    volumes: {}, // volume de cada pessoa (id -> 0..2) e do som da tela de cada uma ("t" + id)
    silenciados: [], // quem eu silenciei só para mim (ids, ou "t" + id para o som da tela)
  },
  notificacoes: {
    area: false, // notificações do sistema quando a aba está em segundo plano
    todas: false, // toda mensagem (não só menções)
  },
}

function mesclar(base, extra) {
  const saida = { ...base }
  for (const [k, v] of Object.entries(extra || {})) {
    saida[k] = v && typeof v === 'object' && !Array.isArray(v) && base[k] ? { ...base[k], ...v } : v
  }
  return saida
}

function ler() {
  try {
    return mesclar(PADRAO, JSON.parse(localStorage.getItem(CHAVE) || '{}'))
  } catch {
    return PADRAO
  }
}

let atual = ler()
const ouvintes = new Set()
let envio = null

function avisar() {
  for (const f of ouvintes) f()
}

function guardarLocal() {
  try {
    localStorage.setItem(CHAVE, JSON.stringify(atual))
  } catch {
    // sem armazenamento: vale até fechar a aba
  }
}

export const preferencias = () => atual

/** Muda uma seção (ex.: mudar('aparencia', { tema: 'neon' })) e salva na conta depois de um instante. */
export function mudar(secao, mudancas) {
  atual = { ...atual, [secao]: { ...atual[secao], ...mudancas } }
  guardarLocal()
  avisar()
  clearTimeout(envio)
  envio = setTimeout(() => api.salvarPreferencias(atual).catch(() => {}), 800)
}

/** Ao entrar na conta: as preferências salvas nela valem mais que as deste navegador. */
export function carregarDaConta(daConta) {
  if (!daConta || typeof daConta !== 'object' || Object.keys(daConta).length === 0) {
    api.salvarPreferencias(atual).catch(() => {}) // primeira vez: leva as deste navegador para a conta
    return
  }
  atual = mesclar(PADRAO, daConta)
  guardarLocal()
  avisar()
}

export function usePreferencias() {
  return useSyncExternalStore(
    (f) => {
      ouvintes.add(f)
      return () => ouvintes.delete(f)
    },
    () => atual,
  )
}

export function aoMudar(f) {
  ouvintes.add(f)
  return () => ouvintes.delete(f)
}
