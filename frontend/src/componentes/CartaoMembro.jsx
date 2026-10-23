import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import CartaoPerfil from './CartaoPerfil.jsx'
import { conversarCom } from './Diretas.jsx'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { corHex } from '../servicos/formato.js'

const CASTIGOS = [
  [5, '5 minutos'],
  [60, '1 hora'],
  [1440, '1 dia'],
  [10080, '1 semana'],
]

export default function CartaoMembro({ visao, usuarioId, aoFechar }) {
  const { estado } = useEstado()
  const sid = visao?.servidor.id
  const m = visao && estado.membros[sid]?.find((x) => x.usuarioId === usuarioId)
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const [completo, setCompleto] = useState(null)

  // bio, banner, pronomes e a relação (amigo, pedido…) vêm do perfil completo
  const [versao, setVersao] = useState(0)
  useEffect(() => {
    api.perfil(usuarioId).then(setCompleto).catch(() => {})
  }, [usuarioId, m?.avatar, m?.statusTexto, versao, estado.amigos])

  const souEu = usuarioId === estado.eu.id
  const perfil = { ...(completo || {}), ...(m || {}), ...(souEu ? estado.eu : {}), usuarioId }
  const onlineAgora = souEu || !!(sid && estado.online[sid]?.[usuarioId]) || !!estado.online[0]?.[usuarioId]
  const acoes = !souEu && completo && (
    <AcoesSociais usuarioId={usuarioId} relacao={completo.relacao} nome={perfil.nomeExibicao} aoFechar={aoFechar} aoMudar={() => setVersao((v) => v + 1)} />
  )
  if (!m) {
    if (!completo && !souEu) return null
    return (
      <Modal titulo={perfil.nomeExibicao} aoFechar={aoFechar} largura="perfil" semTopo>
        <CartaoPerfil perfil={perfil} online={onlineAgora}>
          {acoes}
        </CartaoPerfil>
      </Modal>
    )
  }

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
    <Modal titulo={nomeDe(m)} aoFechar={aoFechar} largura="perfil" semTopo>
      <CartaoPerfil perfil={perfil} online={onlineAgora} apelido={m.apelido} desde={m.entrouEm}>
      {acoes}
      {castigo && <p className="erro">De castigo até {new Date(m.castigadoAte).toLocaleString('pt-BR')}</p>}
      <h4 className="perfil-secao">Cargos</h4>
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
          <h4 className="perfil-secao">Moderação</h4>
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
      </CartaoPerfil>
    </Modal>
  )
}

/** Mensagem direta, amizade e bloqueio, no cartão de perfil de outra pessoa. */
function AcoesSociais({ usuarioId, relacao, nome, aoFechar, aoMudar }) {
  const { despachar, carregarAmigos } = useEstado()
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)

  async function fazer(acao, fechar) {
    setErro('')
    setOcupado(true)
    try {
      await acao()
      carregarAmigos()
      if (fechar) aoFechar()
      else aoMudar()
    } catch (e) {
      setErro(e.message)
    } finally {
      setOcupado(false)
    }
  }

  if (relacao === 'bloqueado') {
    return (
      <div className="acoes-sociais">
        <p className="dica">Você bloqueou esta pessoa.</p>
        <button className="botao" disabled={ocupado} onClick={() => fazer(() => api.desbloquear(usuarioId))}>
          Desbloquear
        </button>
        {erro && <p className="erro">{erro}</p>}
      </div>
    )
  }
  return (
    <div className="acoes-sociais">
      <button className="botao primario" disabled={ocupado} onClick={() => fazer(() => conversarCom(usuarioId, despachar), true)}>
        Mensagem
      </button>
      {relacao === '' && (
        <button className="botao" disabled={ocupado} onClick={() => fazer(() => api.pedirAmizade(usuarioId))}>
          Adicionar amigo
        </button>
      )}
      {relacao === 'pedido_recebido' && (
        <button className="botao sucesso" disabled={ocupado} onClick={() => fazer(() => api.pedirAmizade(usuarioId))}>
          Aceitar amizade
        </button>
      )}
      {relacao === 'pedido_enviado' && (
        <button className="botao" disabled={ocupado} onClick={() => fazer(() => api.removerAmigo(usuarioId))} title="Clique para cancelar o pedido">
          Pedido enviado (cancelar)
        </button>
      )}
      {relacao === 'amigo' && <span className="etiqueta amigos-etiqueta">Amigos</span>}
      <button
        className="botao-link perigo"
        disabled={ocupado}
        onClick={() => confirm(`Bloquear ${nome}? Vocês deixam de ser amigos e essa pessoa não consegue mais mandar mensagem direta para você.`) && fazer(() => api.bloquear(usuarioId))}
      >
        Bloquear
      </button>
      {erro && <p className="erro">{erro}</p>}
    </div>
  )
}
