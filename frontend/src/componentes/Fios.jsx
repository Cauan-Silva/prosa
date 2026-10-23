// Fios (threads): painel lateral com a conversa de um fio e a lista de fios do canal.
import { useEffect, useMemo, useState } from 'react'
import Icone from './Icone.jsx'
import { CanalTexto } from './Conversa.jsx'
import { painelFio, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { P, tem } from '../servicos/permissoes.js'
import { quando } from '../servicos/formato.js'

/** O fio aberto ao lado do canal (mensagens do fio com o mesmo campo de mensagem de sempre). */
export function PainelFio({ visao, canalPai, fio, membros, aoFechar }) {
  const { estado, ouvir } = useEstado()
  const [atual, setAtual] = useState(fio)
  const [renomear, setRenomear] = useState(false)
  const [nome, setNome] = useState(fio.nome)
  const eu = estado.eu.id
  const perm = BigInt(canalPai.permissoes || 0)
  const podeMexer = atual.donoId === eu || tem(perm, P.GerenciarMensagens)

  useEffect(() => setAtual(fio), [fio])
  // nome e contagem mudam pelos eventos; se o fio for apagado, o painel fecha
  useEffect(
    () =>
      ouvir((e) => {
        if (e.t !== 'FIO_ATUALIZADO' || e.d?.id !== fio.id) return
        if (e.d.apagado) aoFechar()
        else setAtual(e.d)
      }),
    [ouvir, fio.id, aoFechar],
  )
  // sem som de mensagem nova para o fio que está aberto
  useEffect(() => {
    painelFio.id = fio.id
    return () => {
      if (painelFio.id === fio.id) painelFio.id = null
    }
  }, [fio.id])

  const canal = useMemo(
    () => ({ id: atual.id, tipo: 'texto', ehFio: true, nome: atual.nome, topico: '', permissoes: canalPai.permissoes }),
    [atual.id, atual.nome, canalPai.permissoes],
  )

  async function salvarNome(e) {
    e?.preventDefault()
    setRenomear(false)
    if (!nome.trim() || nome.trim() === atual.nome) return setNome(atual.nome)
    try {
      setAtual(await api.alterarFio(atual.id, nome.trim()))
    } catch (err) {
      alert(err.message)
      setNome(atual.nome)
    }
  }

  async function apagar() {
    if (!confirm(`Apagar o fio “${atual.nome}” e todas as mensagens dele? Não dá para desfazer.`)) return
    try {
      await api.apagarFio(atual.id)
      aoFechar()
    } catch (err) {
      alert(err.message)
    }
  }

  return (
    <aside className="painel-fio" aria-label={`Fio ${atual.nome}`}>
      <header>
        <Icone nome="fio" tamanho={18} />
        {renomear ? (
          <form onSubmit={salvarNome} className="painel-fio-renomear">
            <input value={nome} onChange={(e) => setNome(e.target.value)} onBlur={salvarNome} maxLength={40} autoFocus aria-label="Nome do fio" />
          </form>
        ) : (
          <h2>
            {podeMexer ? (
              <button className="link-titulo" onClick={() => setRenomear(true)} title="Renomear o fio">
                {atual.nome}
              </button>
            ) : (
              atual.nome
            )}
            <small>em #{canalPai.nome}</small>
          </h2>
        )}
        {podeMexer && (
          <button className="icone-botao perigo" onClick={apagar} aria-label="Apagar o fio" title="Apagar o fio">
            <Icone nome="lixo" tamanho={17} />
          </button>
        )}
        <button className="icone-botao" onClick={aoFechar} aria-label="Fechar o fio" title="Fechar">
          <Icone nome="fechar" />
        </button>
      </header>
      <CanalTexto key={canal.id} visao={visao} canal={canal} membros={membros} />
    </aside>
  )
}

/** Lista dos fios do canal (botão no topo do canal). */
export function ListaFios({ canal, aoAbrir, aoFechar }) {
  const { ouvir } = useEstado()
  const [lista, setLista] = useState(null)
  const [erro, setErro] = useState('')
  const [versao, setVersao] = useState(0)
  useEffect(() => ouvir((e) => e.t === 'FIO_ATUALIZADO' && e.c === canal.id && setVersao((v) => v + 1)), [ouvir, canal.id])
  useEffect(() => {
    api.fios(canal.id).then(setLista).catch((e) => setErro(e.message))
  }, [canal.id, versao])

  return (
    <aside className="busca-painel" aria-label="Fios do canal">
      <header>
        <h2 className="titulo-com-icone">
          <Icone nome="fio" tamanho={18} /> Fios de #{canal.nome}
        </h2>
        <button className="icone-botao" onClick={aoFechar} aria-label="Fechar a lista de fios">
          <Icone nome="fechar" />
        </button>
      </header>
      {!lista && !erro && <p className="aviso-lista">Carregando…</p>}
      {erro && <p className="erro">{erro}</p>}
      {lista?.length === 0 && <p className="aviso-lista">Nenhum fio ainda. Passe o mouse numa mensagem e clique em “Criar fio” para puxar uma conversa paralela.</p>}
      <ul>
        {lista?.map((f) => (
          <li key={f.id}>
            <button onClick={() => aoAbrir(f)}>
              <strong>{f.nome}</strong>
              <small>
                {f.total === 1 ? '1 mensagem' : `${f.total} mensagens`}
                {f.ultimaEm ? ` · última ${quando(f.ultimaEm)}` : ` · criado ${quando(f.criadoEm)}`}
              </small>
            </button>
          </li>
        ))}
      </ul>
    </aside>
  )
}

/** Resumo embaixo da mensagem que abriu um fio. */
export function ResumoFio({ fio, aoAbrir }) {
  return (
    <button className="resumo-fio" onClick={aoAbrir}>
      <Icone nome="fio" tamanho={16} />
      <strong>{fio.nome}</strong>
      <span>
        {fio.total === 0 ? 'Nenhuma mensagem ainda' : fio.total === 1 ? '1 mensagem' : `${fio.total} mensagens`}
        {fio.ultimaEm && ` · ${quando(fio.ultimaEm)}`}
      </span>
      <Icone nome="seta" tamanho={14} />
    </button>
  )
}
