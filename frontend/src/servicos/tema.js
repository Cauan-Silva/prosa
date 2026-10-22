// Tema claro/escuro: segue o sistema até a pessoa escolher um.
const CHAVE = 'prosa.tema'

export function temaAtual() {
  try {
    return localStorage.getItem(CHAVE) || 'sistema'
  } catch {
    return 'sistema'
  }
}

export function aplicarTema(tema = temaAtual()) {
  if (tema === 'sistema') delete document.documentElement.dataset.tema
  else document.documentElement.dataset.tema = tema
}

export function escolherTema(tema) {
  try {
    if (tema === 'sistema') localStorage.removeItem(CHAVE)
    else localStorage.setItem(CHAVE, tema)
  } catch {
    // sem armazenamento: vale até fechar a aba
  }
  aplicarTema(tema)
}
