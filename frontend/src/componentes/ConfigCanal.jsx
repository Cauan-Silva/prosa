import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import { api } from '../servicos/api.js'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { GRUPOS, porChave, big, numero } from '../servicos/permissoes.js'
import { corHex } from '../servicos/formato.js'
import { navegar } from '../servicos/rota.jsx'

const LENTO = [
  [0, 'Desligado'],
  [5, '5 segundos'],
  [10, '10 segundos'],
  [30, '30 segundos'],
  [60, '1 minuto'],
  [300, '5 minutos'],
  [900, '15 minutos'],
  [3600, '1 hora'],
]

/** Cria um canal (sem `canal`) ou edita um existente (nome, tópico, modo lento e permissões). */
export default function ConfigCanal({ visao, canal, categoriaId, tipoInicial, aoFechar }) {
  const novo = !canal
  const [aba, setAba] = useState('geral')
  const sid = visao.servidor.id

  return (
    <Modal titulo={novo ? 'Criar canal' : `Canal ${canal.tipo === 'texto' ? '#' : ''}${canal.nome}`} aoFechar={aoFechar} largura={aba === 'permissoes' ? 'grande' : 'media'}>
      {!novo && (
        <div className="abas" role="tablist">
          <button role="tab" aria-selected={aba === 'geral'} onClick={() => setAba('geral')}>
            Geral
          </button>
          <button role="tab" aria-selected={aba === 'permissoes'} onClick={() => setAba('permissoes')}>
            Permissões
          </button>
        </div>
      )}
      {aba === 'geral' ? (
        <Geral visao={visao} canal={canal} categoriaId={categoriaId} tipoInicial={tipoInicial} aoFechar={aoFechar} />
      ) : (
        <Permissoes sid={sid} visao={visao} canal={canal} />
      )}
    </Modal>
  )
}

function Geral({ visao, canal, categoriaId, tipoInicial, aoFechar }) {
  const { despachar } = useEstado()
  const novo = !canal
  const sid = visao.servidor.id
  const [campos, setCampos] = useState({
    nome: canal?.nome || '',
    tipo: canal?.tipo || tipoInicial || 'texto',
    categoriaId: canal ? canal.categoriaId ?? '' : categoriaId ?? '',
    topico: canal?.topico || '',
    modoLento: canal?.modoLento || 0,
  })
  const [erro, setErro] = useState('')
  const [ocupado, setOcupado] = useState(false)
  const mudar = (c) => (e) => setCampos({ ...campos, [c]: e.target.value })

  async function salvar(e) {
    e.preventDefault()
    setErro('')
    setOcupado(true)
    const cat = campos.categoriaId === '' ? null : Number(campos.categoriaId)
    try {
      if (novo) {
        const c = await api.criarCanal(sid, {
          nome: campos.nome,
          tipo: campos.tipo,
          categoriaId: cat,
          topico: campos.topico,
          modoLento: Number(campos.modoLento),
        })
        // carrega a visão nova antes de abrir o canal (senão a tela ainda não conhece o canal)
        despachar({ tipo: 'visao', id: sid, visao: await api.servidor(sid) })
        aoFechar()
        navegar(`/s/${sid}/${c.id}`)
      } else {
        await api.alterarCanal(sid, canal.id, {
          nome: campos.nome,
          topico: campos.topico,
          modoLento: Number(campos.modoLento),
          ...(cat === null ? { semCategoria: true } : { categoriaId: cat }),
        })
        aoFechar()
      }
    } catch (err) {
      setErro(err.message)
      setOcupado(false)
    }
  }

  async function apagar() {
    if (!confirm(`Apagar o canal ${canal.nome} e todas as mensagens dele? Não dá para desfazer.`)) return
    try {
      await api.apagarCanal(sid, canal.id)
      aoFechar()
    } catch (err) {
      setErro(err.message)
    }
  }

  return (
    <form className="formulario" onSubmit={salvar}>
      {novo && (
        <fieldset className="escolha-tipo">
          <legend>Tipo</legend>
          <label>
            <input type="radio" name="tipo" value="texto" checked={campos.tipo === 'texto'} onChange={mudar('tipo')} />
            <span>
              <strong>Texto</strong>
              <small>Mensagens, arquivos e reações</small>
            </span>
          </label>
          <label>
            <input type="radio" name="tipo" value="voz" checked={campos.tipo === 'voz'} onChange={mudar('tipo')} />
            <span>
              <strong>Voz</strong>
              <small>Conversa por áudio, câmera e tela</small>
            </span>
          </label>
        </fieldset>
      )}
      <label>
        Nome
        <input value={campos.nome} onChange={mudar('nome')} maxLength={40} required autoFocus placeholder={campos.tipo === 'texto' ? 'ex.: avisos' : 'ex.: Sala de estudo'} />
        {campos.tipo === 'texto' && <small>Canais de texto usam letras minúsculas e hífens no lugar dos espaços.</small>}
      </label>
      <label>
        Categoria
        <select value={campos.categoriaId} onChange={mudar('categoriaId')}>
          <option value="">Sem categoria</option>
          {visao.categorias.map((c) => (
            <option key={c.id} value={c.id}>
              {c.nome}
            </option>
          ))}
        </select>
      </label>
      {campos.tipo === 'texto' && (
        <>
          <label>
            Tópico
            <textarea value={campos.topico} onChange={mudar('topico')} maxLength={300} rows={2} placeholder="Do que se fala aqui" />
          </label>
          <label>
            Modo lento
            <select value={campos.modoLento} onChange={mudar('modoLento')}>
              {LENTO.map(([v, r]) => (
                <option key={v} value={v}>
                  {r}
                </option>
              ))}
            </select>
            <small>Cada pessoa espera esse tempo entre uma mensagem e outra. Quem gerencia mensagens não espera.</small>
          </label>
        </>
      )}
      {erro && <p className="erro">{erro}</p>}
      <div className="botoes">
        {!novo && (
          <button type="button" className="botao perigo" onClick={apagar}>
            Apagar canal
          </button>
        )}
        <button className="botao primario" disabled={ocupado}>
          {novo ? 'Criar canal' : 'Salvar'}
        </button>
      </div>
    </form>
  )
}

function Permissoes({ sid, visao, canal }) {
  const { estado } = useEstado()
  const [lista, setLista] = useState(null)
  const [alvo, setAlvo] = useState(null) // { tipo, alvoId }
  const [erro, setErro] = useState('')
  const todos = visao.cargos.find((c) => c.padrao)
  const membros = estado.membros[sid] || []

  const carregar = () =>
    api
      .sobrescritas(sid, canal.id)
      .then((l) => setLista(l))
      .catch((e) => setErro(e.message))

  useEffect(() => {
    carregar()
    setAlvo({ tipo: 'cargo', alvoId: todos.id })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canal.id])

  if (!lista || !alvo) return <p>Carregando…</p>

  const nomeAlvo = (o) =>
    o.tipo === 'cargo' ? visao.cargos.find((c) => c.id === o.alvoId)?.nome || 'cargo apagado' : nomeDe(membros.find((m) => m.usuarioId === o.alvoId))
  const alvos = [{ tipo: 'cargo', alvoId: todos.id }, ...lista.filter((o) => !(o.tipo === 'cargo' && o.alvoId === todos.id))]
  if (!alvos.some((o) => o.tipo === alvo.tipo && o.alvoId === alvo.alvoId)) alvos.push(alvo)
  const atual = lista.find((o) => o.tipo === alvo.tipo && o.alvoId === alvo.alvoId) || { ...alvo, permitir: 0, negar: 0 }
  const usados = new Set(alvos.map((o) => `${o.tipo}${o.alvoId}`))

  async function definir(bit, valor) {
    let permitir = big(atual.permitir) & ~bit
    let negar = big(atual.negar) & ~bit
    if (valor === 'permitir') permitir |= bit
    if (valor === 'negar') negar |= bit
    setErro('')
    try {
      await api.definirSobrescrita(sid, canal.id, { tipo: alvo.tipo, alvoId: alvo.alvoId, permitir: numero(permitir), negar: numero(negar) })
      await carregar()
    } catch (e) {
      setErro(e.message)
    }
  }

  const grupos = canal.tipo === 'voz' ? GRUPOS.filter((g) => g.nome !== 'Servidor' && g.nome !== 'Membros') : GRUPOS.filter((g) => g.nome === 'Texto')

  return (
    <div className="permissoes-canal">
      <div className="alvos">
        <p className="dica">Ajustes valem só neste canal e passam por cima do que o cargo permite no servidor.</p>
        <ul>
          {alvos.map((o) => {
            const cargo = o.tipo === 'cargo' && visao.cargos.find((c) => c.id === o.alvoId)
            return (
              <li key={`${o.tipo}${o.alvoId}`}>
                <button
                  className={o.tipo === alvo.tipo && o.alvoId === alvo.alvoId ? 'ativo' : ''}
                  onClick={() => setAlvo({ tipo: o.tipo, alvoId: o.alvoId })}
                >
                  <span className="bolinha" style={{ background: (cargo && corHex(cargo.cor)) || 'var(--pedra)' }} />
                  {o.tipo === 'membro' && '@'}
                  {nomeAlvo(o)}
                </button>
              </li>
            )
          })}
        </ul>
        <select
          value=""
          onChange={(e) => {
            const [tipo, id] = e.target.value.split(':')
            if (tipo) setAlvo({ tipo, alvoId: Number(id) })
          }}
          aria-label="Adicionar cargo ou pessoa"
        >
          <option value="">Adicionar cargo ou pessoa…</option>
          <optgroup label="Cargos">
            {visao.cargos
              .filter((c) => !c.padrao && !usados.has(`cargo${c.id}`))
              .map((c) => (
                <option key={c.id} value={`cargo:${c.id}`}>
                  {c.nome}
                </option>
              ))}
          </optgroup>
          <optgroup label="Pessoas">
            {membros
              .filter((m) => !usados.has(`membro${m.usuarioId}`))
              .map((m) => (
                <option key={m.usuarioId} value={`membro:${m.usuarioId}`}>
                  {nomeDe(m)}
                </option>
              ))}
          </optgroup>
        </select>
      </div>
      <div className="ajustes">
        <h3>{alvo.tipo === 'membro' ? '@' : ''}{nomeAlvo(alvo)}</h3>
        {grupos.map((g) => (
          <section key={g.nome}>
            <h4>{g.nome}</h4>
            {g.chaves.map((chave) => {
              const p = porChave[chave]
              const valor = big(atual.permitir) & p.bit ? 'permitir' : big(atual.negar) & p.bit ? 'negar' : 'herdar'
              return (
                <div key={chave} className="ajuste">
                  <span>
                    {p.nome}
                    {p.dica && <small>{p.dica}</small>}
                  </span>
                  <div className="tres-estados" role="radiogroup" aria-label={p.nome}>
                    {[
                      ['negar', 'Negar', '✕'],
                      ['herdar', 'Como no cargo', '/'],
                      ['permitir', 'Permitir', '✓'],
                    ].map(([v, r, s]) => (
                      <button key={v} role="radio" aria-checked={valor === v} className={`estado-${v}`} title={r} aria-label={r} onClick={() => valor !== v && definir(p.bit, v)}>
                        {s}
                      </button>
                    ))}
                  </div>
                </div>
              )
            })}
          </section>
        ))}
        {erro && <p className="erro">{erro}</p>}
      </div>
    </div>
  )
}
