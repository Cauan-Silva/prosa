import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import Icone from './Icone.jsx'
import { api } from '../servicos/api.js'

const VALIDADES = [
  [24, '1 dia'],
  [168, '7 dias'],
  [720, '30 dias'],
  [0, 'Nunca expira'],
]

export default function Convidar({ visao, canalId, aoFechar }) {
  const [horas, setHoras] = useState(168)
  const [maxUsos, setMaxUsos] = useState(0)
  const [convite, setConvite] = useState(null)
  const [erro, setErro] = useState('')
  const [copiado, setCopiado] = useState(false)
  const canal = canalId || visao.canais.find((c) => c.tipo === 'texto')?.id

  useEffect(() => {
    setConvite(null)
    api
      .criarConvite(visao.servidor.id, canal, { horas, maxUsos })
      .then(setConvite)
      .catch((e) => setErro(e.message))
  }, [visao.servidor.id, canal, horas, maxUsos])

  const link = convite ? `${location.origin}/convite/${convite.codigo}` : ''

  async function copiar() {
    try {
      await navigator.clipboard.writeText(link)
      setCopiado(true)
      setTimeout(() => setCopiado(false), 2000)
    } catch {
      setErro('Não foi possível copiar. Selecione o link e copie com Ctrl+C.')
    }
  }

  return (
    <Modal titulo={`Convidar para ${visao.servidor.nome}`} aoFechar={aoFechar} largura="pequena">
      <p>Mande este link para quem você quer chamar.</p>
      <div className="link-convite">
        <input readOnly value={link || 'Gerando…'} onFocus={(e) => e.target.select()} aria-label="Link do convite" />
        <button className="botao primario" onClick={copiar} disabled={!convite}>
          <Icone nome="copiar" tamanho={16} /> {copiado ? 'Copiado' : 'Copiar'}
        </button>
      </div>
      <div className="linha-campos">
        <label>
          Vale por
          <select value={horas} onChange={(e) => setHoras(Number(e.target.value))}>
            {VALIDADES.map(([v, r]) => (
              <option key={v} value={v}>
                {r}
              </option>
            ))}
          </select>
        </label>
        <label>
          Quantas pessoas
          <select value={maxUsos} onChange={(e) => setMaxUsos(Number(e.target.value))}>
            {[0, 1, 5, 10, 25, 100].map((v) => (
              <option key={v} value={v}>
                {v === 0 ? 'Sem limite' : v}
              </option>
            ))}
          </select>
        </label>
      </div>
      {erro && <p className="erro">{erro}</p>}
    </Modal>
  )
}
