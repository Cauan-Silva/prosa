import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'
import CartaoMembro from './CartaoMembro.jsx'
import { Avatar, Azulejo } from './Azulejo.jsx'
import { api, enviarIconeServidor } from '../servicos/api.js'
import { recortar } from '../servicos/imagem.js'
import { nomeDe, corDe, useEstado } from '../servicos/estado.jsx'
import { GRUPOS, P, porChave, big, numero, tem } from '../servicos/permissoes.js'
import { corHex, quando } from '../servicos/formato.js'
import { navegar } from '../servicos/rota.jsx'

const CORES = [0x1f3fa6, 0x14635a, 0x2f9e44, 0xe8b420, 0xd9480f, 0xc9472f, 0x9c2f4a, 0x5a2d82, 0x5b6478, 0]

const ABAS = [
  ['geral', 'Geral', P.GerenciarServidor],
  ['cargos', 'Cargos', P.GerenciarCargos],
  ['membros', 'Membros', 0n],
  ['convites', 'Convites', P.GerenciarServidor],
  ['banimentos', 'Banimentos', P.Banir],
  ['auditoria', 'Auditoria', P.VerAuditoria],
]

export default function ConfigServidor({ visao, aba: abaInicial, aoFechar }) {
  const base = BigInt(visao.eu.permissoes)
  const abas = ABAS.filter(([, , p]) => p === 0n || tem(base, p))
  const [aba, setAba] = useState(abaInicial || abas[0][0])

  return (
    <Modal titulo={`Configurações de ${visao.servidor.nome}`} aoFechar={aoFechar} largura="grande">
      <div className="config">
        <nav className="config-abas" role="tablist" aria-orientation="vertical">
          {abas.map(([id, nome]) => (
            <button key={id} role="tab" aria-selected={aba === id} onClick={() => setAba(id)}>
              {nome}
            </button>
          ))}
        </nav>
        <div className="config-conteudo">
          {aba === 'geral' && <Geral visao={visao} aoFechar={aoFechar} />}
          {aba === 'cargos' && <Cargos visao={visao} />}
          {aba === 'membros' && <ListaMembros visao={visao} />}
          {aba === 'convites' && <Convites visao={visao} />}
          {aba === 'banimentos' && <Banimentos visao={visao} />}
          {aba === 'auditoria' && <Auditoria visao={visao} />}
        </div>
      </div>
    </Modal>
  )
}

function Geral({ visao, aoFechar }) {
  const { estado } = useEstado()
  const [nome, setNome] = useState(visao.servidor.nome)
  const [msg, setMsg] = useState('')
  const [enviando, setEnviando] = useState(false)
  const dono = visao.servidor.donoId === estado.eu.id
  const icone = visao.servidor.icone

  async function trocarIcone(arquivo) {
    if (!arquivo) return
    setMsg('')
    setEnviando(true)
    try {
      await enviarIconeServidor(visao.servidor.id, await recortar(arquivo, 256, 256, { manterGif: true }))
      setMsg('Imagem do servidor trocada.')
    } catch (err) {
      setMsg(err.message || 'Não foi possível usar essa imagem.')
    } finally {
      setEnviando(false)
    }
  }

  async function tirarIcone() {
    setMsg('')
    try {
      await api.apagarIconeServidor(visao.servidor.id)
      setMsg('Imagem removida: o servidor voltou a usar o azulejo.')
    } catch (err) {
      setMsg(err.message)
    }
  }

  async function salvar(e) {
    e.preventDefault()
    try {
      await api.alterarServidor(visao.servidor.id, { nome: nome.trim() })
      setMsg('Nome salvo.')
    } catch (err) {
      setMsg(err.message)
    }
  }

  async function apagar() {
    const confirma = prompt(`Para apagar o servidor e tudo o que tem nele, digite o nome: ${visao.servidor.nome}`)
    if (confirma === null) return
    if (confirma.trim() !== visao.servidor.nome) {
      setMsg('O nome não confere. Nada foi apagado.')
      return
    }
    try {
      await api.apagarServidor(visao.servidor.id)
      aoFechar()
      navegar('/')
    } catch (err) {
      setMsg(err.message)
    }
  }

  return (
    <>
      <form className="formulario" onSubmit={salvar}>
        <div className="previa-azulejo">
          <Azulejo nome={nome.trim() || visao.servidor.nome} src={icone} tamanho={88} />
          <div className="previa-azulejo-acoes">
            <div className="linha-botoes">
              <label className="botao">
                {enviando ? 'Enviando…' : icone ? 'Trocar imagem' : 'Escolher imagem'}
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/webp,image/gif"
                  hidden
                  disabled={enviando}
                  onChange={(e) => {
                    trocarIcone(e.target.files[0])
                    e.target.value = ''
                  }}
                />
              </label>
              {icone && (
                <button type="button" className="botao" onClick={tirarIcone}>
                  Remover imagem
                </button>
              )}
            </div>
            <p>{icone ? 'PNG, JPG, WebP ou GIF animado, até 4 MB. Ela é recortada em quadrado.' : 'Sem imagem, o servidor usa um azulejo desenhado a partir do nome.'}</p>
          </div>
        </div>
        <label>
          Nome do servidor
          <input value={nome} onChange={(e) => setNome(e.target.value)} minLength={2} maxLength={60} required />
        </label>
        <button className="botao primario">Salvar</button>
        {msg && <p role="status">{msg}</p>}
      </form>
      {dono && (
        <section className="zona-perigo">
          <h3>Apagar servidor</h3>
          <p>Apaga canais, mensagens, cargos e arquivos. Não dá para desfazer.</p>
          <button className="botao perigo" onClick={apagar}>
            Apagar servidor
          </button>
        </section>
      )}
    </>
  )
}

function Cargos({ visao }) {
  const { estado } = useEstado()
  const sid = visao.servidor.id
  const ordenados = [...visao.cargos].sort((a, b) => b.posicao - a.posicao)
  const [selecionado, setSelecionado] = useState(ordenados[0]?.id)
  const [erro, setErro] = useState('')
  const dono = visao.servidor.donoId === estado.eu.id
  const minhaPos = dono ? Infinity : Math.max(0, ...visao.cargos.filter((c) => visao.eu.cargos.includes(c.id)).map((c) => c.posicao))
  const cargo = visao.cargos.find((c) => c.id === selecionado) || ordenados[0]

  async function criar() {
    setErro('')
    try {
      const c = await api.criarCargo(sid, { nome: 'Novo cargo', permissoes: numero(big(visao.cargos.find((x) => x.padrao).permissoes)) })
      setSelecionado(c.id)
    } catch (e) {
      setErro(e.message)
    }
  }

  async function mover(c, direcao) {
    const semTodos = ordenados.filter((x) => !x.padrao).map((x) => x.id)
    const i = semTodos.indexOf(c.id)
    const j = i + direcao
    if (j < 0 || j >= semTodos.length) return
    ;[semTodos[i], semTodos[j]] = [semTodos[j], semTodos[i]]
    setErro('')
    try {
      await api.ordenarCargos(sid, semTodos)
    } catch (e) {
      setErro(e.message)
    }
  }

  return (
    <div className="cargos">
      <div className="cargos-lista">
        <button className="botao primario" onClick={criar}>
          Criar cargo
        </button>
        <ol>
          {ordenados.map((c, i) => {
            const editavel = !c.padrao && (dono || c.posicao < minhaPos)
            return (
              <li key={c.id} className={c.id === cargo?.id ? 'ativo' : ''}>
                <button className="cargo-nome" onClick={() => setSelecionado(c.id)}>
                  <span className="bolinha" style={{ background: corHex(c.cor) || 'var(--pedra)' }} />
                  {c.nome}
                </button>
                {editavel && (
                  <span className="mover">
                    <button className="icone-botao" disabled={i === 0} onClick={() => mover(c, -1)} aria-label={`Subir ${c.nome}`}>
                      ↑
                    </button>
                    <button className="icone-botao" disabled={ordenados[i + 1]?.padrao} onClick={() => mover(c, 1)} aria-label={`Descer ${c.nome}`}>
                      ↓
                    </button>
                  </span>
                )}
              </li>
            )
          })}
        </ol>
        <p className="dica">Quem está mais acima manda em quem está abaixo. A cor do nome vem do cargo mais alto.</p>
        {erro && <p className="erro">{erro}</p>}
      </div>
      {cargo && <EditarCargo key={cargo.id} visao={visao} cargo={cargo} editavel={cargo.padrao ? tem(BigInt(visao.eu.permissoes), P.GerenciarCargos) : dono || cargo.posicao < minhaPos} />}
    </div>
  )
}

function EditarCargo({ visao, cargo, editavel }) {
  const sid = visao.servidor.id
  const [campos, setCampos] = useState({ nome: cargo.nome, cor: cargo.cor, permissoes: big(cargo.permissoes), mencionavel: cargo.mencionavel })
  const [msg, setMsg] = useState('')
  const minhas = BigInt(visao.eu.permissoes)
  const mudou =
    campos.nome !== cargo.nome || campos.cor !== cargo.cor || campos.permissoes !== big(cargo.permissoes) || campos.mencionavel !== cargo.mencionavel

  async function salvar() {
    setMsg('')
    try {
      await api.alterarCargo(sid, cargo.id, {
        ...(cargo.padrao ? {} : { nome: campos.nome.trim(), cor: campos.cor, mencionavel: campos.mencionavel }),
        permissoes: numero(campos.permissoes),
      })
      setMsg('Cargo salvo.')
    } catch (e) {
      setMsg(e.message)
    }
  }

  async function apagar() {
    if (!confirm(`Apagar o cargo ${cargo.nome}? Quem tem o cargo perde as permissões dele.`)) return
    try {
      await api.apagarCargo(sid, cargo.id)
    } catch (e) {
      setMsg(e.message)
    }
  }

  return (
    <div className="cargo-edicao">
      {cargo.padrao ? (
        <p className="dica">@todos é o cargo de todo mundo no servidor. O que ele permite vale para todos os membros.</p>
      ) : (
        <>
          <label>
            Nome
            <input value={campos.nome} disabled={!editavel} onChange={(e) => setCampos({ ...campos, nome: e.target.value })} maxLength={40} />
          </label>
          <fieldset className="cores" disabled={!editavel}>
            <legend>Cor</legend>
            {CORES.map((c) => (
              <button
                key={c}
                type="button"
                className={`cor ${campos.cor === c ? 'escolhida' : ''}`}
                style={{ background: corHex(c) || 'transparent' }}
                onClick={() => setCampos({ ...campos, cor: c })}
                aria-label={c ? `Cor ${corHex(c)}` : 'Sem cor'}
                aria-pressed={campos.cor === c}
              >
                {c === 0 && '∅'}
              </button>
            ))}
            <input
              type="color"
              value={corHex(campos.cor) || '#5b6478'}
              onChange={(e) => setCampos({ ...campos, cor: parseInt(e.target.value.slice(1), 16) })}
              aria-label="Outra cor"
            />
          </fieldset>
          <label className="caixa">
            <input type="checkbox" disabled={!editavel} checked={campos.mencionavel} onChange={(e) => setCampos({ ...campos, mencionavel: e.target.checked })} />
            Qualquer pessoa pode mencionar este cargo
          </label>
        </>
      )}
      {GRUPOS.map((g) => (
        <fieldset key={g.nome} className="grupo-permissoes" disabled={!editavel}>
          <legend>{g.nome}</legend>
          {g.chaves.map((chave) => {
            const p = porChave[chave]
            const ligado = (campos.permissoes & p.bit) !== 0n
            const semDireito = !tem(minhas, p.bit)
            return (
              <label key={chave} className="interruptor">
                <span>
                  {p.nome}
                  {p.dica && <small>{p.dica}</small>}
                </span>
                <input
                  type="checkbox"
                  role="switch"
                  checked={ligado}
                  disabled={semDireito}
                  title={semDireito ? 'Você não tem esta permissão, então não pode dar nem tirar' : undefined}
                  onChange={() => setCampos({ ...campos, permissoes: campos.permissoes ^ p.bit })}
                />
              </label>
            )
          })}
        </fieldset>
      ))}
      {editavel && (
        <div className="botoes barra-salvar">
          {!cargo.padrao && (
            <button className="botao perigo" onClick={apagar}>
              Apagar cargo
            </button>
          )}
          <button className="botao primario" onClick={salvar} disabled={!mudou}>
            Salvar cargo
          </button>
        </div>
      )}
      {msg && <p role="status">{msg}</p>}
    </div>
  )
}

function ListaMembros({ visao }) {
  const { estado } = useEstado()
  const [filtro, setFiltro] = useState('')
  const [aberto, setAberto] = useState(null)
  const lista = (estado.membros[visao.servidor.id] || [])
    .filter((m) => [m.apelido, m.nomeExibicao, m.nomeUsuario].some((n) => n?.toLowerCase().includes(filtro.toLowerCase())))
    .sort((a, b) => nomeDe(a).localeCompare(nomeDe(b), 'pt-BR'))
  return (
    <>
      <input className="filtro" value={filtro} onChange={(e) => setFiltro(e.target.value)} placeholder="Procurar membro" aria-label="Procurar membro" />
      <ul className="tabela-membros">
        {lista.map((m) => (
          <li key={m.usuarioId}>
            <button onClick={() => setAberto(m.usuarioId)}>
              <Avatar nome={m.nomeExibicao} id={m.usuarioId} src={m.avatar} tamanho={32} />
              <span>
                <strong style={{ color: corDe(m, visao.cargos) || undefined }}>{nomeDe(m)}</strong>
                <small>@{m.nomeUsuario}</small>
              </span>
              <span className="chips">
                {visao.cargos
                  .filter((c) => !c.padrao && m.cargos.includes(c.id))
                  .map((c) => (
                    <span key={c.id} className="chip" style={{ '--cor-chip': corHex(c.cor) || 'var(--pedra)' }}>
                      {c.nome}
                    </span>
                  ))}
              </span>
            </button>
          </li>
        ))}
      </ul>
      {aberto && <CartaoMembro visao={visao} usuarioId={aberto} aoFechar={() => setAberto(null)} />}
    </>
  )
}

function useCarregar(fn, deps) {
  const [dados, setDados] = useState(null)
  const [erro, setErro] = useState('')
  const recarregar = () => fn().then(setDados).catch((e) => setErro(e.message))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => void recarregar(), deps)
  return [dados, erro, recarregar]
}

function Convites({ visao }) {
  const sid = visao.servidor.id
  const [lista, erro, recarregar] = useCarregar(() => api.convites(sid), [sid])
  return (
    <>
      {erro && <p className="erro">{erro}</p>}
      {lista?.length === 0 && <p className="aviso-lista">Nenhum convite ativo.</p>}
      <ul className="tabela">
        {lista?.map((c) => (
          <li key={c.codigo}>
            <code>{c.codigo}</code>
            <span>
              por {c.criadorNome}, {c.usos}
              {c.maxUsos ? `/${c.maxUsos}` : ''} usos
            </span>
            <small>{c.expiraEm ? `expira ${quando(c.expiraEm)}` : 'não expira'}</small>
            <button className="botao perigo pequeno" onClick={() => api.apagarConvite(sid, c.codigo).then(recarregar)}>
              Revogar
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}

function Banimentos({ visao }) {
  const sid = visao.servidor.id
  const [lista, erro, recarregar] = useCarregar(() => api.banidos(sid), [sid])
  return (
    <>
      {erro && <p className="erro">{erro}</p>}
      {lista?.length === 0 && <p className="aviso-lista">Ninguém banido.</p>}
      <ul className="tabela">
        {lista?.map((b) => (
          <li key={b.usuarioId}>
            <strong>{b.nomeExibicao}</strong>
            <small>@{b.nomeUsuario}</small>
            <span>{b.motivo || 'sem motivo'}</span>
            <button className="botao pequeno" onClick={() => api.desbanir(sid, b.usuarioId).then(recarregar)}>
              Desbanir
            </button>
          </li>
        ))}
      </ul>
    </>
  )
}

const ACOES = {
  membro_expulso: 'expulsou',
  membro_banido: 'baniu',
  membro_desbanido: 'desbaniu',
  membro_castigado: 'castigou',
  castigo_retirado: 'tirou do castigo',
  mensagem_apagada: 'apagou uma mensagem',
  permissoes_canal: 'mudou permissões do canal',
  canal_criado: 'criou o canal',
  canal_alterado: 'alterou o canal',
  canal_apagado: 'apagou o canal',
  cargo_criado: 'criou o cargo',
  cargo_alterado: 'alterou o cargo',
  cargo_apagado: 'apagou o cargo',
  cargo_dado: 'deu cargo',
  cargo_tirado: 'tirou cargo',
  servidor_alterado: 'alterou o servidor',
  icone_alterado: 'trocou a imagem do servidor',
  icone_removido: 'removeu a imagem do servidor',
  apelido_alterado: 'mudou apelido',
}

function Auditoria({ visao }) {
  const sid = visao.servidor.id
  const [lista, erro] = useCarregar(() => api.auditoria(sid), [sid])
  return (
    <>
      {erro && <p className="erro">{erro}</p>}
      {lista?.length === 0 && <p className="aviso-lista">Nada registrado ainda.</p>}
      <ol className="auditoria">
        {lista?.map((a) => (
          <li key={a.id}>
            <p>
              <strong>{a.autorNome || 'Conta apagada'}</strong> {ACOES[a.acao] || a.acao.replaceAll('_', ' ')} {a.alvo && <em>{a.alvo}</em>}
            </p>
            {a.detalhes && <small>{a.detalhes}</small>}
            <time dateTime={a.criadoEm}>{quando(a.criadoEm)}</time>
          </li>
        ))}
      </ol>
    </>
  )
}
