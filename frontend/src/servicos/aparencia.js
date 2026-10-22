// Temas e personalização visual. Tudo vira variáveis CSS no <html>, então o app inteiro
// muda de cor de uma vez, sem recarregar.
import { aoMudar, preferencias } from './preferencias.js'

const claro = {
  giz: '#f4f6fb', papel: '#ffffff', anil: '#16226b', 'anil-2': '#1d2b80', cobalto: '#1f3fa6',
  'cobalto-texto': '#1f3fa6', 'cobalto-claro': '#e5eafb', ipe: '#e8b420', 'ipe-claro': '#fdf3d3',
  texto: '#19203a', pedra: '#5b6478', linha: '#dde2ee', perigo: '#c2362b', verde: '#2f9e44',
  'azulejo-fundo': '#f7f8fb', sombra: '0 10px 40px rgb(22 34 107 / 0.18)', esquema: 'light',
}

const escuro = {
  ...claro, giz: '#10162f', papel: '#151c3a', anil: '#0a0f26', 'anil-2': '#141b44', cobalto: '#5b78f0',
  'cobalto-texto': '#9db0ff', 'cobalto-claro': '#222c5c', 'ipe-claro': '#3b3215', texto: '#e7eaf6',
  pedra: '#9aa3bd', linha: '#283160', perigo: '#ff7b6e', 'azulejo-fundo': '#e9ecf6',
  sombra: '0 10px 40px rgb(0 0 0 / 0.5)', esquema: 'dark',
}

export const TEMAS = [
  { id: 'azulejo-claro', nome: 'Azulejo', cores: claro },
  { id: 'azulejo-escuro', nome: 'Azulejo noturno', cores: escuro },
  {
    id: 'meia-noite', nome: 'Meia-noite',
    cores: { ...escuro, giz: '#0d0b26', papel: '#120f33', anil: '#07061a', 'anil-2': '#1a1550', cobalto: '#7c5cff',
      'cobalto-texto': '#b9a8ff', 'cobalto-claro': '#251d5e', ipe: '#5ad1ff', 'ipe-claro': '#123049',
      texto: '#ece9ff', pedra: '#a39fcc', linha: '#2a2463', verde: '#3ddc97' },
  },
  {
    id: 'neon', nome: 'Neon',
    cores: { ...escuro, giz: '#0a0a12', papel: '#0f0f1a', anil: '#050508', 'anil-2': '#16162a', cobalto: '#ff2bd6',
      'cobalto-texto': '#ff7ae8', 'cobalto-claro': '#2d0f2a', ipe: '#22f0ff', 'ipe-claro': '#0b2e33',
      texto: '#f4f4ff', pedra: '#9d9db8', linha: '#25253d', verde: '#39ff8f' },
  },
  {
    id: 'grafite', nome: 'Grafite',
    cores: { ...escuro, giz: '#17181b', papel: '#1e1f23', anil: '#111214', 'anil-2': '#26272c', cobalto: '#f2994a',
      'cobalto-texto': '#ffb877', 'cobalto-claro': '#33291f', ipe: '#f2c94c', 'ipe-claro': '#332b14',
      texto: '#ececee', pedra: '#a0a1a8', linha: '#2e2f35' },
  },
  {
    id: 'floresta', nome: 'Floresta',
    cores: { ...escuro, giz: '#0f1a15', papel: '#13221b', anil: '#0a120e', 'anil-2': '#173326', cobalto: '#3fbf7f',
      'cobalto-texto': '#7ee0ad', 'cobalto-claro': '#1c3a2c', ipe: '#e7c55a', 'ipe-claro': '#2f2a14',
      texto: '#e6f2ea', pedra: '#9cb5a7', linha: '#22382d' },
  },
  {
    id: 'cafe', nome: 'Café',
    cores: { ...claro, giz: '#f5efe7', papel: '#fffaf3', anil: '#3b2a20', 'anil-2': '#55392a', cobalto: '#8a4b2a',
      'cobalto-texto': '#7a3f20', 'cobalto-claro': '#f0e1d2', ipe: '#d9a13b', 'ipe-claro': '#fbeccc',
      texto: '#2e221b', pedra: '#7a675b', linha: '#e6d8c8', 'azulejo-fundo': '#fbf6ef' },
  },
  {
    id: 'rosa', nome: 'Rosa',
    cores: { ...claro, giz: '#fbf1f5', papel: '#ffffff', anil: '#5a1838', 'anil-2': '#7a2350', cobalto: '#d63a7a',
      'cobalto-texto': '#b8285f', 'cobalto-claro': '#fbe0eb', ipe: '#7c5cff', 'ipe-claro': '#ece6ff',
      texto: '#2b1220', pedra: '#7d6070', linha: '#f0d9e3' },
  },
  {
    id: 'alto-contraste', nome: 'Alto contraste',
    cores: { ...escuro, giz: '#000000', papel: '#000000', anil: '#000000', 'anil-2': '#1a1a1a', cobalto: '#ffd400',
      'cobalto-texto': '#ffe866', 'cobalto-claro': '#332b00', ipe: '#00e5ff', 'ipe-claro': '#002a30',
      texto: '#ffffff', pedra: '#d0d0d0', linha: '#ffffff', perigo: '#ff6b6b', verde: '#00ff7f' },
  },
]

export const FONTES = [
  { id: 'schibsted', nome: 'Schibsted Grotesk', css: "'Schibsted Grotesk', ui-sans-serif, system-ui, sans-serif" },
  { id: 'atkinson', nome: 'Atkinson (leitura fácil)', css: "'Atkinson Hyperlegible', ui-sans-serif, system-ui, sans-serif" },
  { id: 'nunito', nome: 'Nunito (arredondada)', css: "'Nunito', ui-sans-serif, system-ui, sans-serif" },
  { id: 'mono', nome: 'JetBrains Mono', css: "'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace" },
  { id: 'sistema', nome: 'Do sistema', css: "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif" },
]

const escuroDoSistema = window.matchMedia('(prefers-color-scheme: dark)')

export function temaEfetivo(id) {
  if (id === 'sistema') return escuroDoSistema.matches ? TEMAS[1] : TEMAS[0]
  return TEMAS.find((t) => t.id === id) || TEMAS[0]
}

function hexParaRgb(hex) {
  const n = parseInt(hex.replace('#', ''), 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function misturar(hex, outro, quanto) {
  const a = hexParaRgb(hex)
  const b = hexParaRgb(outro)
  const c = a.map((v, i) => Math.round(v * (1 - quanto) + b[i] * quanto))
  return `#${c.map((v) => v.toString(16).padStart(2, '0')).join('')}`
}

/** As cores finais do tema, já com a cor de destaque escolhida pela pessoa. */
export function coresDe(ap) {
  const tema = temaEfetivo(ap.tema)
  const cores = { ...tema.cores }
  if (/^#[0-9a-f]{6}$/i.test(ap.destaque || '')) {
    const d = ap.destaque
    const escuroTema = cores.esquema === 'dark'
    cores.cobalto = d
    cores['cobalto-texto'] = escuroTema ? misturar(d, '#ffffff', 0.35) : misturar(d, '#000000', 0.15)
    cores['cobalto-claro'] = misturar(cores.papel, d, escuroTema ? 0.25 : 0.14)
  }
  return cores
}

const carregadas = new Set(['schibsted'])
function carregarFonte(id) {
  if (carregadas.has(id) || id === 'sistema') return
  carregadas.add(id)
  const familias = { atkinson: 'Atkinson+Hyperlegible:wght@400;700', nunito: 'Nunito:wght@400;500;700;800', mono: 'JetBrains+Mono:wght@400;500;700' }
  const link = document.createElement('link')
  link.rel = 'stylesheet'
  link.href = `https://fonts.googleapis.com/css2?family=${familias[id]}&display=swap`
  document.head.appendChild(link)
}

export function aplicarAparencia(ap = preferencias().aparencia) {
  const raiz = document.documentElement
  const cores = coresDe(ap)
  for (const [k, v] of Object.entries(cores)) {
    if (k === 'esquema') continue
    raiz.style.setProperty(`--${k}`, v)
  }
  raiz.style.colorScheme = cores.esquema
  const fonte = FONTES.find((f) => f.id === ap.fonte) || FONTES[0]
  carregarFonte(fonte.id)
  raiz.style.setProperty('--fonte', fonte.css)
  raiz.style.setProperty('--raio', ap.cantos === 'retos' ? '3px' : '10px')
  raiz.style.zoom = ap.escala && ap.escala !== 1 ? String(ap.escala) : ''
  raiz.classList.toggle('compacta', ap.densidade === 'compacta')
  raiz.classList.toggle('cantos-retos', ap.cantos === 'retos')
  raiz.classList.toggle('sem-animacoes', ap.animacoes === false)
  raiz.classList.toggle('sublinhar-links', !!ap.sublinharLinks)
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', cores.anil)
}

let iniciado = false
export function iniciarAparencia() {
  if (iniciado) return
  iniciado = true
  aplicarAparencia()
  aoMudar(() => aplicarAparencia())
  escuroDoSistema.addEventListener?.('change', () => aplicarAparencia())
}
