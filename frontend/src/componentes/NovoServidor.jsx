import { useState } from 'react'
import Modal from './Modal.jsx'
import { Azulejo } from './Azulejo.jsx'
import { api } from '../servicos/api.js'
import { navegar } from '../servicos/rota.jsx'
import { useEstado } from '../servicos/estado.jsx'

export default function NovoServidor({ aoFechar }) {
  const { despachar } = useEstado()
  const [nome, setNome] = useState('')
  const [convite, setConvite] = useState('')
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)

  async function criar(e) {
    e.preventDefault()
    setOcupado(true)
    setErro('')
    try {
      const v = await api.criarServidor(nome.trim())
      despachar({ tipo: 'visao', id: v.servidor.id, visao: v })
      aoFechar()
      navegar(`/s/${v.servidor.id}`)
    } catch (err) {
      setErro(err.message)
      setOcupado(false)
    }
  }

  function entrar(e) {
    e.preventDefault()
    const codigo = convite.trim().split('/').filter(Boolean).at(-1)
    if (!codigo) return
    aoFechar()
    navegar(`/convite/${codigo}`)
  }

  return (
    <Modal titulo="Criar ou entrar num servidor" aoFechar={aoFechar}>
      <form onSubmit={criar} className="formulario novo-servidor">
        <div className="previa-azulejo">
          <Azulejo nome={nome.trim() || 'Seu servidor'} tamanho={88} />
          <p>O desenho do azulejo sai do nome. Cada nome tem o seu.</p>
        </div>
        <label>
          Nome do servidor
          <input value={nome} onChange={(e) => setNome(e.target.value)} minLength={2} maxLength={60} required autoFocus placeholder="Ex.: Turma do café" />
        </label>
        <button className="botao primario" disabled={ocupado || nome.trim().length < 2}>
          Criar servidor
        </button>
      </form>
      <hr />
      <form onSubmit={entrar} className="formulario">
        <label>
          Recebeu um convite?
          <input value={convite} onChange={(e) => setConvite(e.target.value)} placeholder="Cole o link ou o código" />
        </label>
        <button className="botao" disabled={!convite.trim()}>
          Ver convite
        </button>
      </form>
      {erro && <p className="erro">{erro}</p>}
    </Modal>
  )
}
