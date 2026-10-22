import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import { Azulejo } from './Azulejo.jsx'
import { api } from '../servicos/api.js'
import { navegar } from '../servicos/rota.jsx'
import { useEstado } from '../servicos/estado.jsx'

export default function AceitarConvite({ codigo, aoFechar }) {
  const { estado, despachar } = useEstado()
  const [convite, setConvite] = useState(null)
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)

  useEffect(() => {
    api.verConvite(codigo).then(setConvite).catch((e) => setErro(e.message))
  }, [codigo])

  const jaMembro = convite && estado.servidores[convite.servidorId]

  async function aceitar() {
    setOcupado(true)
    try {
      if (!jaMembro) {
        await api.aceitarConvite(codigo)
        despachar({ tipo: 'visao', id: convite.servidorId, visao: await api.servidor(convite.servidorId) })
      }
      navegar(`/s/${convite.servidorId}`, { substituir: true })
    } catch (e) {
      setErro(e.message)
      setOcupado(false)
    }
  }

  return (
    <Modal titulo="Convite" aoFechar={aoFechar} largura="pequena">
      {!convite && !erro && <p>Abrindo o convite…</p>}
      {convite && (
        <div className="convite-previa">
          <Azulejo nome={convite.servidorNome} tamanho={96} />
          <p>
            <strong>{convite.criadorNome}</strong> chamou você para
          </p>
          <h3>{convite.servidorNome}</h3>
          {convite.totalMembros > 0 && <p>{convite.totalMembros} {convite.totalMembros === 1 ? 'membro' : 'membros'}</p>}
          <button className="botao primario grande" onClick={aceitar} disabled={ocupado}>
            {jaMembro ? 'Abrir o servidor' : 'Aceitar convite'}
          </button>
        </div>
      )}
      {erro && <p className="erro">{erro}</p>}
    </Modal>
  )
}
