import { useState } from 'react'
import Modal from './Modal.jsx'
import { Avatar } from './Azulejo.jsx'
import { api } from '../servicos/api.js'
import { useSessao } from '../servicos/sessao.jsx'
import { useEstado } from '../servicos/estado.jsx'
import { configSons, mudarSons, tocar } from '../servicos/sons.js'

export default function Perfil({ aoFechar }) {
  const { sair, setUsuario } = useSessao()
  const { estado, despachar } = useEstado()
  const [nome, setNome] = useState(estado.eu.nomeExibicao)
  const [msg, setMsg] = useState('')
  const [sons, setSons] = useState(configSons)
  const mudar = (m) => {
    mudarSons(m)
    setSons(configSons())
  }

  async function salvar(e) {
    e.preventDefault()
    try {
      const u = await api.alterarPerfil({ nomeExibicao: nome.trim() })
      setUsuario(u)
      despachar({ tipo: 'eu', usuario: u })
      setMsg('Nome salvo.')
    } catch (err) {
      setMsg(err.message)
    }
  }

  return (
    <Modal titulo="Seu perfil" aoFechar={aoFechar} largura="pequena">
      <div className="cartao-membro">
        <Avatar nome={estado.eu.nomeExibicao} id={estado.eu.id} tamanho={72} />
        <div>
          <strong>{estado.eu.nomeExibicao}</strong>
          <p>@{estado.eu.nomeUsuario}</p>
          <p>{estado.eu.email}</p>
        </div>
      </div>
      <form className="formulario" onSubmit={salvar}>
        <label>
          Como quer ser chamado
          <input value={nome} onChange={(e) => setNome(e.target.value)} maxLength={40} required />
        </label>
        <button className="botao primario">Salvar nome</button>
        {msg && <p role="status">{msg}</p>}
      </form>
      <hr />
      <h3>Sons</h3>
      <div className="formulario">
        <label className="caixa">
          <input type="checkbox" checked={sons.ligado} onChange={(e) => mudar({ ligado: e.target.checked })} />
          Tocar sons (mensagens, menções, voz e botões)
        </label>
        <label>
          Volume
          <input
            type="range"
            min="0"
            max="1"
            step="0.05"
            value={sons.volume}
            disabled={!sons.ligado}
            onChange={(e) => mudar({ volume: Number(e.target.value) })}
            onPointerUp={() => tocar('mencao')}
            onKeyUp={() => tocar('mencao')}
          />
        </label>
      </div>
      <hr />
      <button className="botao perigo" onClick={sair}>
        Sair da conta
      </button>
    </Modal>
  )
}
