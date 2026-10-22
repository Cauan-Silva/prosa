import { useState } from 'react'
import { Avatar } from './Azulejo.jsx'
import CartaoMembro from './CartaoMembro.jsx'
import { corDe, nomeDe, useEstado } from '../servicos/estado.jsx'

export default function Membros({ visao }) {
  const { estado } = useEstado()
  const [aberto, setAberto] = useState(null)
  const sid = visao.servidor.id
  const lista = estado.membros[sid]
  const online = estado.online[sid] || {}

  if (!lista) {
    return (
      <aside className="membros">
        <p className="aviso-lista">Carregando membros…</p>
      </aside>
    )
  }

  const ordenar = (a, b) => nomeDe(a).localeCompare(nomeDe(b), 'pt-BR')
  const on = lista.filter((m) => online[m.usuarioId] || m.usuarioId === estado.eu.id).sort(ordenar)
  const off = lista.filter((m) => !(online[m.usuarioId] || m.usuarioId === estado.eu.id)).sort(ordenar)

  const item = (m, estaOnline) => {
    const cor = corDe(m, visao.cargos)
    const castigo = m.castigadoAte && new Date(m.castigadoAte) > new Date()
    return (
      <li key={m.usuarioId}>
        <button className={`membro ${estaOnline ? '' : 'offline'}`} onClick={() => setAberto(m.usuarioId)}>
          <Avatar nome={m.nomeExibicao} id={m.usuarioId} tamanho={32} online={estaOnline} />
          <span style={cor ? { color: cor } : undefined}>{nomeDe(m)}</span>
          {m.usuarioId === visao.servidor.donoId && <small className="etiqueta">dono</small>}
          {castigo && <small className="etiqueta perigo">castigo</small>}
        </button>
      </li>
    )
  }

  return (
    <aside className="membros" aria-label="Membros">
      <h2>Online ({on.length})</h2>
      <ul>{on.map((m) => item(m, true))}</ul>
      {off.length > 0 && (
        <>
          <h2>Offline ({off.length})</h2>
          <ul>{off.map((m) => item(m, false))}</ul>
        </>
      )}
      {aberto && <CartaoMembro visao={visao} usuarioId={aberto} aoFechar={() => setAberto(null)} />}
    </aside>
  )
}
