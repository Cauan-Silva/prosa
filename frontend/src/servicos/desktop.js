// Ponte com o programa para Windows (Tauri). Dentro do programa, o site ganha
// window.__TAURI__: o "pressionar para falar" passa a funcionar com o Prosa em segundo
// plano (até com jogo em tela cheia) e dá para abrir o Prosa junto com o Windows.
// No navegador comum, nada aqui faz efeito.

const tauri = typeof window !== 'undefined' ? window.__TAURI__ : undefined

export const noDesktop = !!tauri?.core?.invoke

const invocar = (cmd, args) => tauri.core.invoke(cmd, args)

/** Pede ao programa para vigiar a tecla (código do KeyboardEvent, ex.: "KeyV"); "" desliga. */
export async function definirPttGlobal(codigo) {
  if (!noDesktop) return false
  try {
    return await invocar('definir_ptt', { codigo: codigo || '' })
  } catch {
    return false
  }
}

/** Avisa quando a tecla do "pressionar para falar" é apertada ou solta (em qualquer janela). */
export function aoPttGlobal(fn) {
  if (!noDesktop || !tauri.event?.listen) return () => {}
  let desligar = null
  let vivo = true
  tauri.event.listen('prosa-ptt', (e) => fn(!!e.payload)).then((u) => (vivo ? (desligar = u) : u()))
  return () => {
    vivo = false
    desligar?.()
  }
}

export const inicioAutomatico = {
  ligado: () => invocar('plugin:autostart|is_enabled'),
  mudar: (ligar) => invocar(ligar ? 'plugin:autostart|enable' : 'plugin:autostart|disable'),
}

export const versaoDesktop = () => (noDesktop ? invocar('versao_desktop').catch(() => '') : Promise.resolve(''))

export const LINK_DOWNLOAD = 'https://github.com/Cauan-Silva/prosa/releases/latest'
