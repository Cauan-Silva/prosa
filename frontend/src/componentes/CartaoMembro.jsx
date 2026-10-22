import { useState } from 'react'
import Modal from './Modal.jsx'
import { Avatar } from './Azulejo.jsx'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { corHex, formatarDia } from '../servicos/formato.js'

const CASTIGOS = [
  [5, '5 minutos'],
  [60, '1 hora'],
  [1440, '1 dia'],
  [10080, '1 semana'],
]

export default function CartaoMembro({ visao, usuarioId, aoFechar }) {
  const { estado } = useEstado()
  const sid = visao.servidor.id
  const m = estado.membros[sid]?.find((x) => x.usuarioId === usuarioId)
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  if (!m) return null

  const eu = estado.eu.id
  const souDono = visao.servidor.donoId === eu
  const base = BigInt(visao.eu.permissoes)
  const posicao = (ids) => Math.max(0, ...visao.cargos.filter((c) => ids.includes(c.id)).map((c) => c.posicao))
  const minhaPos = souDono ? Infinity : posicao(visao.eu.cargos)
  const delePos = m.usuarioId === visao.servidor.donoId ? Infinity : posicao(m.cargos)
  const acima = m.usuarioId !== eu && minhaPos > delePos
  const castigo = m.castigadoAte && new Date(m.castigadoAte) > new Date()

  async function fazer(acao) {
    setErro('')
    setOcupado(true)
    try {
      await acao()
    } catch (e) {
      setErro(e.message)
    } finally {
      setOcupado(false)
    }
  }

  const cargosDele = visao.cargos.filter((c) => !c.padrao && m.cargos.includes(c.id)).sort((a, b) => b.posicao - a.posicao)
  const podeDarCargo = tem(base, P.GerenciarCargos)
  const disponiveis = visao.cargos.filter((c) => !c.padrao && !m.cargos.includes(c.id) && (souDono || c.posicao < minhaPos))

  return (
    <Modal titulo={nomeDe(m)} aoFechar={aoFechar} largura="pequena">
      <div className="cartao-membro">
        <Avatar nome={m.nomeExibicao} id={m.usuarioId} tamanho={72} online={!!estado.online[sid]?.[m.usuarioId] || m.usuarioId === eu} />
        <div>
          <strong>{m.nomeExibicao}</strong>
          <p>@{m.nomeUsuario}</p>
          {m.apelido && <p>Apelido aqui: {m.apelido}</p>}
          <p>No servidor desde {formatarDia(m.entrouEm).toLowerCase()}</p>
          {castigo && <p className="erro">De castigo até {new Date(m.castigadoAte).toLocaleString('pt-BR')}</p>}
        </div>
      </div>

      <h3>Cargos</h3>
      <ul className="chips">
        {cargosDele.length === 0 && <li className="vazio">Nenhum cargo além de @todos.</li>}
        {cargosDele.map((c) => (
          <li key={c.id} className="chip" style={{ '--cor-chip': corHex(c.cor) || 'var(--pedra)' }}>
            {c.nome}
            {podeDarCargo && (souDono || c.posicao < minhaPos) && (
              <button disabled={ocupado} onClick={() => fazer(() => api.tirarCargo(sid, m.usuarioId, c.id))} aria-label={`Tirar ${c.nome}`}>
                ×
              </button>
            )}
          </li>
        ))}
      </ul>
      {podeDarCargo && disponiveis.length > 0 && (
        <select
          value=""
          disabled={ocupado}
          onChange={(e) => e.target.value && fazer(() => api.darCargo(sid, m.usuarioId, Number(e.target.value)))}
          aria-label="Dar cargo"
        >
          <option value="">Dar um cargo…</option>
          {disponiveis.map((c) => (
            <option key={c.id} value={c.id}>
              {c.nome}
            </option>
          ))}
        </select>
      )}

      {(m.usuarioId === eu ? tem(base, P.AlterarApelido) : acima && tem(base, P.GerenciarApelidos)) && (
        <button
          className="botao"
          disabled={ocupado}
          onClick={() => {
            const novo = prompt('Apelido neste servidor (vazio tira o apelido)', m.apelido || '')
            if (novo !== null) fazer(() => api.apelido(sid, m.usuarioId, novo.trim()))
          }}
        >
          Mudar apelido
        </button>
      )}

      {acima && (tem(base, P.Castigar) || tem(base, P.Expulsar) || tem(base, P.Banir)) && (
        <>
          <h3>Moderação</h3>
          <div className="moderacao">
            {tem(base, P.Castigar) &&
              (castigo ? (
                <button className="botao" disabled={ocupado} onClick={() => fazer(() => api.castigar(sid, m.usuarioId, 0, ''))}>
                  Tirar do castigo
                </button>
              ) : (
                <select
                  value=""
                  disabled={ocupado}
                  aria-label="Castigar"
                  onChange={(e) => {
                    const minutos = Number(e.target.value)
                    if (!minutos) return
                    const motivo = prompt('Motivo (aparece no registro de auditoria)') ?? null
                    if (motivo !== null) fazer(() => api.castigar(sid, m.usuarioId, minutos, motivo))
                  }}
                >
                  <option value="">Castigar por…</option>
                  {CASTIGOS.map(([v, r]) => (
                    <option key={v} value={v}>
                      {r}
                    </option>
                  ))}
                </select>
              ))}
            {tem(base, P.Expulsar) && (
              <button
                className="botao perigo"
                disabled={ocupado}
                onClick={() => {
                  const motivo = prompt(`Expulsar ${nomeDe(m)}? Ela pode voltar com um convite. Motivo:`)
                  if (motivo !== null) fazer(async () => (await api.expulsar(sid, m.usuarioId, motivo), aoFechar()))
                }}
              >
                Expulsar
              </button>
            )}
            {tem(base, P.Banir) && (
              <button
                className="botao perigo"
                disabled={ocupado}
                onClick={() => {
                  const motivo = prompt(`Banir ${nomeDe(m)}? Ela não consegue voltar até ser desbanida. Motivo:`)
                  if (motivo !== null) fazer(async () => (await api.banir(sid, m.usuarioId, motivo), aoFechar()))
                }}
              >
                Banir
              </button>
            )}
          </div>
        </>
      )}
      {erro && <p className="erro">{erro}</p>}
    </Modal>
  )
}
