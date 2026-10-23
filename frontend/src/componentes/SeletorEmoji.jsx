// Seletor de emojis: os do sistema (os mais usados) e os dos servidores da pessoa.
import { useEffect, useRef, useState } from 'react'
import { emojisDisponiveis, useEstado } from '../servicos/estado.jsx'

export const COMUNS = [
  '😀', '😂', '🤣', '😊', '😍', '🥰', '😎', '🤔', '😅', '😭', '😡', '😱', '🥺', '😴', '🤯', '🙃',
  '👍', '👎', '👏', '🙌', '🙏', '💪', '🤝', '👀', '❤️', '🔥', '✨', '🎉', '💯', '✅', '❌', '⭐',
  '☕', '🍕', '🎮', '📚', '🎵', '💻', '⚽', '🚀',
]

/**
 * aoEscolher recebe { texto } para emoji do sistema ou { emoji } para emoji de servidor.
 * soServidor limita aos emojis de um servidor (reações).
 */
export default function SeletorEmoji({ servidorId, soServidor, aoEscolher, aoFechar, classe = '' }) {
  const { estado } = useEstado()
  const [busca, setBusca] = useState('')
  const ref = useRef(null)
  useEffect(() => {
    const fora = (e) => !ref.current?.contains(e.target) && aoFechar()
    const esc = (e) => e.key === 'Escape' && aoFechar()
    document.addEventListener('pointerdown', fora)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('pointerdown', fora)
      document.removeEventListener('keydown', esc)
    }
  }, [aoFechar])

  const b = busca.trim().toLowerCase().replace(/:/g, '')
  let proprios = emojisDisponiveis(estado, servidorId)
  if (soServidor) proprios = proprios.filter((e) => e.servidorId === servidorId)
  proprios = proprios.filter((e) => !b || e.nome.toLowerCase().includes(b))
  const grupos = []
  for (const e of proprios) {
    let g = grupos.find((x) => x.id === e.servidorId)
    if (!g) grupos.push((g = { id: e.servidorId, nome: e.servidor, lista: [] }))
    g.lista.push(e)
  }

  return (
    <div className={`seletor-emoji-painel ${classe}`} ref={ref} role="dialog" aria-label="Escolher emoji">
      <input value={busca} onChange={(e) => setBusca(e.target.value)} placeholder="Buscar emoji do servidor" aria-label="Buscar emoji" autoFocus />
      <div className="seletor-emoji-rolagem">
        {grupos.map((g) => (
          <section key={g.id}>
            <h4>{g.nome}</h4>
            <div className="seletor-emoji-grade">
              {g.lista.map((e) => (
                <button key={e.id} type="button" onClick={() => aoEscolher({ emoji: e })} title={`:${e.nome}:`} aria-label={`:${e.nome}:`}>
                  <img src={e.url} alt="" loading="lazy" />
                </button>
              ))}
            </div>
          </section>
        ))}
        {!b && (
          <section>
            <h4>Comuns</h4>
            <div className="seletor-emoji-grade">
              {COMUNS.map((t) => (
                <button key={t} type="button" onClick={() => aoEscolher({ texto: t })} aria-label={t}>
                  {t}
                </button>
              ))}
            </div>
          </section>
        )}
        {b && grupos.length === 0 && <p className="dica">Nenhum emoji com esse nome nos seus servidores.</p>}
      </div>
    </div>
  )
}
