// Enquetes: criar (janela) e votar (cartão dentro da mensagem).
import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import Icone from './Icone.jsx'
import { api } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { formatarCompleto } from '../servicos/formato.js'

const DURACOES = [
  [1, '1 hora'],
  [4, '4 horas'],
  [8, '8 horas'],
  [24, '1 dia'],
  [72, '3 dias'],
  [168, '1 semana'],
  [0, 'Sem prazo'],
]

export function CriarEnquete({ canal, aoCriar, aoFechar }) {
  const [pergunta, setPergunta] = useState('')
  const [opcoes, setOpcoes] = useState(['', ''])
  const [multipla, setMultipla] = useState(false)
  const [horas, setHoras] = useState(24)
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const preenchidas = opcoes.filter((o) => o.trim()).length

  function mudarOpcao(i, valor) {
    const novas = opcoes.map((o, j) => (j === i ? valor : o))
    // sempre sobra um campo vazio no fim (até 10)
    if (i === novas.length - 1 && valor.trim() && novas.length < 10) novas.push('')
    setOpcoes(novas)
  }

  async function criar(e) {
    e.preventDefault()
    setErro('')
    setOcupado(true)
    try {
      const m = await api.enviar(canal.id, { conteudo: '', enquete: { pergunta, opcoes, multipla, horas } })
      aoCriar(m)
      aoFechar()
    } catch (err) {
      setErro(err.message)
    } finally {
      setOcupado(false)
    }
  }

  return (
    <Modal titulo="Criar enquete" aoFechar={aoFechar} largura="pequena">
      <form className="formulario criar-enquete" onSubmit={criar}>
        <label>
          Pergunta
          <input value={pergunta} onChange={(e) => setPergunta(e.target.value)} maxLength={300} placeholder="Ex.: Que dia fica melhor para estudar?" required autoFocus />
        </label>
        <fieldset>
          <legend>Opções</legend>
          {opcoes.map((o, i) => (
            <div key={i} className="opcao-campo">
              <input value={o} onChange={(e) => mudarOpcao(i, e.target.value)} maxLength={80} placeholder={`Opção ${i + 1}`} aria-label={`Opção ${i + 1}`} />
              {opcoes.length > 2 && o && (
                <button type="button" className="icone-botao" onClick={() => setOpcoes(opcoes.filter((_, j) => j !== i))} aria-label={`Tirar opção ${i + 1}`}>
                  <Icone nome="fechar" tamanho={14} />
                </button>
              )}
            </div>
          ))}
        </fieldset>
        <label className="interruptor">
          <span>
            Permitir escolher mais de uma
            <small>Cada pessoa pode marcar várias opções</small>
          </span>
          <input type="checkbox" role="switch" checked={multipla} onChange={(e) => setMultipla(e.target.checked)} />
        </label>
        <label>
          Duração
          <select value={horas} onChange={(e) => setHoras(Number(e.target.value))}>
            {DURACOES.map(([v, r]) => (
              <option key={v} value={v}>
                {r}
              </option>
            ))}
          </select>
        </label>
        {erro && <p className="erro">{erro}</p>}
        <button className="botao primario" disabled={ocupado || !pergunta.trim() || preenchidas < 2}>
          {ocupado ? 'Criando…' : 'Publicar enquete'}
        </button>
      </form>
    </Modal>
  )
}

/** Quanto falta para acabar ("termina em 3 h"). */
function falta(fim) {
  const ms = new Date(fim) - Date.now()
  if (ms <= 0) return null
  const min = Math.ceil(ms / 60000)
  if (min < 60) return `${min} min`
  const h = Math.round(min / 60)
  if (h < 48) return `${h} h`
  return `${Math.round(h / 24)} dias`
}

export function CartaoEnquete({ m, canal, eu }) {
  const e = m.enquete
  const [agora, setAgora] = useState(Date.now())
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  // marca como encerrada sozinha quando o prazo passa (sem precisar recarregar)
  useEffect(() => {
    if (!e.encerraEm || e.encerrada) return
    const t = setInterval(() => setAgora(Date.now()), 30000)
    return () => clearInterval(t)
  }, [e.encerraEm, e.encerrada])
  const encerrada = e.encerrada || (e.encerraEm && new Date(e.encerraEm) <= agora)
  const minhas = e.opcoes.map((o, i) => (o.votos.includes(eu) ? i : -1)).filter((i) => i >= 0)
  const pessoas = new Set(e.opcoes.flatMap((o) => o.votos)).size
  const maior = Math.max(0, ...e.opcoes.map((o) => o.votos.length))
  const perm = BigInt(canal.permissoes || 0)
  const podeEncerrar = !encerrada && (m.autor?.id === eu || tem(perm, P.GerenciarMensagens))
  const mostrarResultado = encerrada || minhas.length > 0

  async function votar(i) {
    if (encerrada || ocupado) return
    let novas
    if (e.multipla) novas = minhas.includes(i) ? minhas.filter((x) => x !== i) : [...minhas, i]
    else novas = minhas.includes(i) ? [] : [i]
    setErro('')
    setOcupado(true)
    try {
      await api.votar(canal.id, m.id, novas)
    } catch (err) {
      setErro(err.message)
    } finally {
      setOcupado(false)
    }
  }

  async function encerrar() {
    if (!confirm('Encerrar a enquete agora? Ninguém mais vai poder votar.')) return
    try {
      await api.encerrarEnquete(canal.id, m.id)
    } catch (err) {
      setErro(err.message)
    }
  }

  return (
    <div className={`enquete ${encerrada ? 'encerrada' : ''}`}>
      <div className="enquete-topo">
        <Icone nome="enquete" tamanho={16} />
        <strong>{e.pergunta}</strong>
      </div>
      <small className="dica">{encerrada ? 'Resultado final' : e.multipla ? 'Escolha uma ou mais opções' : 'Escolha uma opção'}</small>
      <ul role={e.multipla ? 'group' : 'radiogroup'} aria-label={e.pergunta}>
        {e.opcoes.map((o, i) => {
          const minha = minhas.includes(i)
          const pct = pessoas ? Math.round((o.votos.length / pessoas) * 100) : 0
          const vencedora = encerrada && maior > 0 && o.votos.length === maior
          return (
            <li key={i}>
              <button
                className={`enquete-opcao ${minha ? 'minha' : ''} ${vencedora ? 'vencedora' : ''}`}
                onClick={() => votar(i)}
                disabled={encerrada}
                role={e.multipla ? 'checkbox' : 'radio'}
                aria-checked={minha}
              >
                {mostrarResultado && <span className="enquete-barra" style={{ width: `${pct}%` }} aria-hidden="true" />}
                <span className="enquete-marca" aria-hidden="true">
                  {minha && <Icone nome="check" tamanho={14} />}
                </span>
                <span className="enquete-texto">{o.texto}</span>
                {mostrarResultado && (
                  <span className="enquete-numeros">
                    {o.votos.length} · {pct}%
                  </span>
                )}
              </button>
            </li>
          )
        })}
      </ul>
      <div className="enquete-rodape">
        <span>
          {pessoas === 1 ? '1 pessoa votou' : `${pessoas} pessoas votaram`}
          {' · '}
          {encerrada ? 'enquete encerrada' : e.encerraEm ? <span title={formatarCompleto(e.encerraEm)}>termina em {falta(e.encerraEm)}</span> : 'sem prazo'}
        </span>
        {!mostrarResultado && pessoas > 0 && <span className="dica">Vote para ver o resultado</span>}
        {podeEncerrar && (
          <button className="botao-link" onClick={encerrar}>
            Encerrar agora
          </button>
        )}
      </div>
      {erro && <p className="erro">{erro}</p>}
    </div>
  )
}
