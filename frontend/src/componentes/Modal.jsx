import { useEffect, useRef } from 'react'
import Icone from './Icone.jsx'

/** Janela sobre a tela, usando <dialog> (foco preso, Esc fecha, leitor de tela entende). */
export default function Modal({ titulo, aoFechar, children, largura = 'media', rodape, semTopo }) {
  const ref = useRef(null)
  useEffect(() => {
    const d = ref.current
    if (!d.open) d.showModal()
    const cancelar = (e) => {
      e.preventDefault()
      aoFechar()
    }
    d.addEventListener('cancel', cancelar)
    return () => d.removeEventListener('cancel', cancelar)
  }, [aoFechar])
  return (
    <dialog
      ref={ref}
      className={`modal modal-${largura} ${semTopo ? 'modal-sem-topo' : ''}`}
      aria-label={titulo}
      onPointerDown={(e) => e.target === ref.current && aoFechar()}
    >
      <div className="modal-caixa">
        {semTopo ? (
          <button className="icone-botao modal-fechar-flutuante" onClick={aoFechar} aria-label="Fechar">
            <Icone nome="fechar" />
          </button>
        ) : (
          <header className="modal-topo">
            <h2>{titulo}</h2>
            <button className="icone-botao" onClick={aoFechar} aria-label="Fechar">
              <Icone nome="fechar" />
            </button>
          </header>
        )}
        <div className={`modal-corpo ${semTopo ? 'sem-topo' : ''}`}>{children}</div>
        {rodape && <footer className="modal-rodape">{rodape}</footer>}
      </div>
    </dialog>
  )
}
