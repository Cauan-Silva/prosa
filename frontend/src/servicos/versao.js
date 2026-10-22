// Avisa quando sai uma versão nova do site (depois de um deploy), para ninguém ficar
// usando a antiga sem saber. Compara os arquivos que o index.html carrega (os nomes
// mudam a cada build) com os da página aberta.
import { useSyncExternalStore } from 'react'

let original = null
let nova = false
const ouvintes = new Set()

function assinatura(doc) {
  return [...doc.querySelectorAll('script[src*="/assets/"], link[href*="/assets/"]')]
    .map((el) => el.getAttribute('src') || el.getAttribute('href'))
    .sort()
    .join('|')
}

let conferindo = false
export async function verificarVersao() {
  if (nova || conferindo || original === '') return
  conferindo = true
  try {
    const r = await fetch('/', { cache: 'no-store', headers: { Accept: 'text/html' } })
    if (!r.ok) return
    const atual = assinatura(new DOMParser().parseFromString(await r.text(), 'text/html'))
    if (atual && atual !== original) {
      nova = true
      for (const f of ouvintes) f()
    }
  } catch {
    // sem rede: confere na próxima
  } finally {
    conferindo = false
  }
}

export function iniciarVersao() {
  original = assinatura(document) // '' no modo de desenvolvimento (sem /assets/): não confere
  if (!original) return
  setInterval(verificarVersao, 5 * 60 * 1000)
  document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && verificarVersao())
}

export const useNovaVersao = () =>
  useSyncExternalStore(
    (f) => (ouvintes.add(f), () => ouvintes.delete(f)),
    () => nova,
  )
