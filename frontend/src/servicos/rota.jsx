// Roteador mínimo com a History API: três telas não justificam uma biblioteca.
import { useEffect, useState } from 'react'

export function navegar(caminho, { substituir = false } = {}) {
  if (caminho === location.pathname) return
  history[substituir ? 'replaceState' : 'pushState'](null, '', caminho)
  window.dispatchEvent(new Event('rota'))
  window.scrollTo(0, 0)
}

export function useCaminho() {
  const [caminho, setCaminho] = useState(location.pathname)
  useEffect(() => {
    const mudar = () => setCaminho(location.pathname)
    window.addEventListener('popstate', mudar)
    window.addEventListener('rota', mudar)
    return () => {
      window.removeEventListener('popstate', mudar)
      window.removeEventListener('rota', mudar)
    }
  }, [])
  return caminho
}

/** Link que troca de tela sem recarregar a página (Ctrl+clique continua abrindo em outra aba). */
export function Link({ para, children, ...resto }) {
  return (
    <a
      href={para}
      onClick={(e) => {
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
        e.preventDefault()
        navegar(para)
      }}
      {...resto}
    >
      {children}
    </a>
  )
}
