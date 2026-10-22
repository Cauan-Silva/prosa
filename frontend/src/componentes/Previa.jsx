import { useEffect, useState } from 'react'
import { api } from '../servicos/api.js'

// Uma busca por link na sessão inteira (várias mensagens com o mesmo link pedem uma vez).
const cache = new Map()
function buscar(url) {
  if (!cache.has(url)) cache.set(url, api.previa(url).catch(() => null))
  return cache.get(url)
}

/** Cartão com título, descrição e imagem do site, embaixo da mensagem. */
export default function Previa({ url }) {
  const [p, setP] = useState(null)
  const [semImagem, setSemImagem] = useState(false)
  useEffect(() => {
    let vivo = true
    buscar(url).then((r) => vivo && setP(r))
    return () => {
      vivo = false
    }
  }, [url])
  if (!p) return null
  const soImagem = p.imagem && !p.titulo && !p.descricao
  if (soImagem) {
    return semImagem ? null : (
      <a className="previa previa-so-imagem" href={url} target="_blank" rel="noopener noreferrer nofollow">
        <img src={p.imagem} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setSemImagem(true)} />
      </a>
    )
  }
  return (
    <div className="previa" style={p.cor ? { '--cor-previa': p.cor } : undefined}>
      <div className="previa-texto">
        {p.site && <small className="previa-site">{p.site}</small>}
        {p.titulo && (
          <a className="previa-titulo" href={url} target="_blank" rel="noopener noreferrer nofollow">
            {p.titulo}
          </a>
        )}
        {p.descricao && <p className="previa-descricao">{p.descricao}</p>}
      </div>
      {p.imagem && !semImagem && (
        <a className="previa-imagem" href={url} target="_blank" rel="noopener noreferrer nofollow" tabIndex={-1}>
          <img src={p.imagem} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setSemImagem(true)} />
        </a>
      )}
    </div>
  )
}
