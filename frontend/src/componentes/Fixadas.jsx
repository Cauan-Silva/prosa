import { useEffect, useState } from 'react'
import Icone from './Icone.jsx'
import { irParaMensagem } from './Mensagem.jsx'
import { Texto } from '../servicos/texto.jsx'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { api } from '../servicos/api.js'
import { quando } from '../servicos/formato.js'

/** Painel com as mensagens fixadas do canal (abre pelo alfinete no topo). */
export default function Fixadas({ visao, canal, membros, aoFechar }) {
  const { estado } = useEstado()
  const [lista, setLista] = useState(null)
  const [erro, setErro] = useState('')
  const [aviso, setAviso] = useState('')
  // recarrega quando alguém fixa ou desafixa uma mensagem já carregada neste canal
  const versao = (estado.mensagens[canal.id]?.lista || [])
    .filter((m) => m.fixadaEm)
    .map((m) => m.id)
    .join(',')

  useEffect(() => {
    api.fixadas(canal.id).then(setLista).catch((e) => setErro(e.message))
  }, [canal.id, versao])

  return (
    <aside className="busca-painel fixadas-painel" aria-label="Mensagens fixadas">
      <header>
        <h2>
          <Icone nome="fixar" tamanho={18} /> Fixadas
        </h2>
        <button className="icone-botao" onClick={aoFechar} aria-label="Fechar mensagens fixadas">
          <Icone nome="fechar" />
        </button>
      </header>
      {!lista && !erro && <p className="aviso-lista">Carregando…</p>}
      {erro && <p className="erro">{erro}</p>}
      {aviso && <p className="aviso-lista">{aviso}</p>}
      {lista?.length === 0 && <p className="aviso-lista">Nenhuma mensagem fixada ainda. Quem modera o canal pode fixar pelo alfinete de cada mensagem.</p>}
      <ul>
        {lista?.map((m) => (
          <li key={m.id}>
            <button
              onClick={() => setAviso(irParaMensagem(m.id) ? '' : 'Essa mensagem é antiga: role o canal para cima até ela aparecer.')}
            >
              <small>{quando(m.criadaEm)}</small>
              <strong>{m.autor ? nomeDe(membros?.find((x) => x.usuarioId === m.autor.id) || m.autor) : 'Conta apagada'}</strong>
              <span className="busca-texto">
                {m.conteudo ? <Texto conteudo={m.conteudo} membros={membros} cargos={visao.cargos} eu={estado.eu} /> : <em>(só anexos)</em>}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </aside>
  )
}
