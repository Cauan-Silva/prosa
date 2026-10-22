import { useEffect, useState } from 'react'
import Icone from './Icone.jsx'
import { Texto } from '../servicos/texto.jsx'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { quando } from '../servicos/formato.js'
import { navegar } from '../servicos/rota.jsx'

export default function Busca({ visao, termo, aoFechar }) {
  const { estado } = useEstado()
  const [resultado, setResultado] = useState(null)
  const [erro, setErro] = useState('')
  const sid = visao.servidor.id
  const membros = estado.membros[sid]

  useEffect(() => {
    setResultado(null)
    setErro('')
    api.buscar(sid, { q: termo }).then(setResultado).catch((e) => setErro(e.message))
  }, [sid, termo])

  return (
    <aside className="busca-painel" aria-label="Resultados da busca">
      <header>
        <h2>“{termo}”</h2>
        <button className="icone-botao" onClick={aoFechar} aria-label="Fechar busca">
          <Icone nome="fechar" />
        </button>
      </header>
      {!resultado && !erro && <p className="aviso-lista">Buscando…</p>}
      {erro && <p className="erro">{erro}</p>}
      {resultado?.length === 0 && <p className="aviso-lista">Nada encontrado. Tente outras palavras.</p>}
      <ul>
        {resultado?.map((m) => {
          const canal = visao.canais.find((c) => c.id === m.canalId)
          return (
            <li key={m.id}>
              <button onClick={() => navegar(`/s/${sid}/${m.canalId}`)}>
                <small>
                  #{canal?.nome}, {quando(m.criadaEm)}
                </small>
                <strong>{m.autor ? nomeDe(membros?.find((x) => x.usuarioId === m.autor.id) || m.autor) : 'Conta apagada'}</strong>
                <span className="busca-texto">
                  <Texto conteudo={m.conteudo} membros={membros} cargos={visao.cargos} eu={estado.eu} />
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </aside>
  )
}
