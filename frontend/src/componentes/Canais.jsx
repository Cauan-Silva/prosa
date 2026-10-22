import { useEffect, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import { Avatar } from './Azulejo.jsx'
import { useEstado } from '../servicos/estado.jsx'
import { P, tem } from '../servicos/permissoes.js'
import { api } from '../servicos/api.js'
import { navegar } from '../servicos/rota.jsx'
import { escolherTema, temaAtual } from '../servicos/tema.js'
import { useVoz } from '../servicos/voz.jsx'
import { Controles } from './SalaVoz.jsx'

const PODE_CONFIGURAR = P.GerenciarServidor | P.GerenciarCargos | P.GerenciarCanais | P.Expulsar | P.Banir | P.VerAuditoria

export default function Canais({ visao, canalId, aoEscolher, aoAbrir }) {
  const { estado } = useEstado()
  const [menu, setMenu] = useState(false)
  const [recolhidas, setRecolhidas] = useState({})
  const menuRef = useRef(null)
  const base = BigInt(visao.eu.permissoes)
  const servidorId = visao.servidor.id
  const dono = visao.servidor.donoId === estado.eu.id

  useEffect(() => {
    if (!menu) return
    const fora = (e) => !menuRef.current?.contains(e.target) && setMenu(false)
    document.addEventListener('pointerdown', fora)
    return () => document.removeEventListener('pointerdown', fora)
  }, [menu])

  const categorias = [...visao.categorias].sort((a, b) => a.posicao - b.posicao)
  const grupos = [{ id: null, nome: null }, ...categorias].map((cat) => ({
    ...cat,
    canais: visao.canais.filter((c) => (c.categoriaId ?? null) === cat.id).sort((a, b) => a.posicao - b.posicao),
  }))
  const gerenciaCanais = tem(base, P.GerenciarCanais)

  async function sair() {
    setMenu(false)
    if (!confirm(`Sair de ${visao.servidor.nome}? Você só volta com um convite novo.`)) return
    try {
      await api.sair(servidorId)
      navegar('/')
    } catch (e) {
      alert(e.message)
    }
  }

  return (
    <aside className="canais" aria-label={`Canais de ${visao.servidor.nome}`}>
      <div className="canais-topo" ref={menuRef}>
        <button className="canais-titulo" onClick={() => setMenu((m) => !m)} aria-expanded={menu} aria-haspopup="menu">
          <span>{visao.servidor.nome}</span>
          <Icone nome={menu ? 'fechar' : 'seta'} tamanho={18} />
        </button>
        {menu && (
          <div className="menu" role="menu">
            {tem(base, P.CriarConvite) && (
              <button role="menuitem" onClick={() => (setMenu(false), aoAbrir('convidar'))}>
                <Icone nome="convite" tamanho={18} /> Convidar pessoas
              </button>
            )}
            {(base & PODE_CONFIGURAR) !== 0n && (
              <button role="menuitem" onClick={() => (setMenu(false), aoAbrir('config-servidor'))}>
                <Icone nome="config" tamanho={18} /> Configurações do servidor
              </button>
            )}
            {gerenciaCanais && (
              <button role="menuitem" onClick={() => (setMenu(false), aoAbrir('novo-canal'))}>
                <Icone nome="mais" tamanho={18} /> Criar canal
              </button>
            )}
            {gerenciaCanais && (
              <button
                role="menuitem"
                onClick={async () => {
                  setMenu(false)
                  const nome = prompt('Nome da categoria')
                  if (nome?.trim()) api.criarCategoria(servidorId, nome.trim()).catch((e) => alert(e.message))
                }}
              >
                <Icone nome="menu" tamanho={18} /> Criar categoria
              </button>
            )}
            {!dono && (
              <button role="menuitem" className="perigo" onClick={sair}>
                <Icone nome="sair" tamanho={18} /> Sair do servidor
              </button>
            )}
          </div>
        )}
      </div>

      <div className="canais-lista">
        {grupos.map((g) => {
          if (g.id !== null && g.canais.length === 0 && !gerenciaCanais) return null
          const fechada = recolhidas[g.id]
          return (
            <section key={g.id ?? 'sem'} className="grupo">
              {g.nome && (
                <div className="grupo-titulo">
                  <button onClick={() => setRecolhidas({ ...recolhidas, [g.id]: !fechada })} aria-expanded={!fechada}>
                    <Icone nome="seta" tamanho={14} />
                    {g.nome}
                  </button>
                  {gerenciaCanais && (
                    <button className="icone-botao" onClick={() => aoAbrir('novo-canal', { categoriaId: g.id })} aria-label={`Criar canal em ${g.nome}`} title="Criar canal">
                      <Icone nome="mais" tamanho={16} />
                    </button>
                  )}
                </div>
              )}
              {g.canais
                .filter((c) => !fechada || c.id === canalId || ((c.ultimaMensagemId || 0) > (c.ultimaLida || 0)))
                .map((c) => (
                  <ItemCanal key={c.id} canal={c} ativo={c.id === canalId} aoEscolher={aoEscolher} aoAbrir={aoAbrir} voz={estado.voz[servidorId]?.[c.id]} membros={estado.membros[servidorId]} />
                ))}
            </section>
          )
        })}
      </div>

      <BarraVoz />
      <Rodape aoAbrir={aoAbrir} />
    </aside>
  )
}

function ItemCanal({ canal, ativo, aoEscolher, aoAbrir, voz, membros }) {
  const naoLido = canal.tipo === 'texto' && (canal.ultimaMensagemId || 0) > (canal.ultimaLida || 0)
  const p = BigInt(canal.permissoes || 0)
  return (
    <div className={`canal ${ativo ? 'ativo' : ''} ${naoLido ? 'nao-lido' : ''}`}>
      <button
        className="canal-nome"
        onClick={() => aoEscolher(canal.id)}
        aria-current={ativo ? 'page' : undefined}
      >
        <Icone nome={canal.tipo === 'voz' ? 'voz' : 'hashtag'} tamanho={18} />
        <span>{canal.nome}</span>
        {canal.mencoesNaoLidas > 0 && <span className="selo">{canal.mencoesNaoLidas}</span>}
      </button>
      {tem(p, P.GerenciarCanais) && (
        <button className="icone-botao canal-config" onClick={() => aoAbrir('config-canal', { canalId: canal.id })} aria-label={`Configurar ${canal.nome}`} title="Configurar canal">
          <Icone nome="config" tamanho={16} />
        </button>
      )}
      {canal.tipo === 'voz' && voz?.length > 0 && (
        <ul className="voz-pessoas">
          {voz.map((v) => {
            const m = membros?.find((x) => x.usuarioId === v.usuarioId)
            return (
              <li key={v.usuarioId} className={v.falando ? 'falando' : ''}>
                <Avatar nome={m?.nomeExibicao || v.nome} id={v.usuarioId} tamanho={22} />
                <span>{m?.apelido || m?.nomeExibicao || v.nome}</span>
                {v.mudo && <Icone nome="microfone-off" tamanho={14} />}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

function Rodape({ aoAbrir }) {
  const { estado } = useEstado()
  const [tema, setTema] = useState(temaAtual)
  const proximo = { sistema: 'escuro', escuro: 'claro', claro: 'sistema' }
  const rotulos = { sistema: 'Tema do sistema', escuro: 'Tema escuro', claro: 'Tema claro' }
  return (
    <div className="rodape-usuario">
      <button className="rodape-eu" onClick={() => aoAbrir('perfil')} title="Seu perfil">
        <Avatar nome={estado.eu.nomeExibicao} id={estado.eu.id} tamanho={34} online />
        <span>
          <strong>{estado.eu.nomeExibicao}</strong>
          <small>@{estado.eu.nomeUsuario}</small>
        </span>
      </button>
      <button
        className="icone-botao"
        onClick={() => {
          const t = proximo[tema]
          escolherTema(t)
          setTema(t)
        }}
        title={`${rotulos[tema]} (clique para trocar)`}
        aria-label={rotulos[tema]}
      >
        <Icone nome={tema === 'claro' ? 'sol' : tema === 'escuro' ? 'lua' : 'bussola'} tamanho={18} />
      </button>
    </div>
  )
}

function BarraVoz() {
  const { estado } = useEstado()
  const voz = useVoz()
  if (!voz.canalId) return null
  const v = estado.servidores[voz.servidorId]
  const canal = v?.canais.find((c) => c.id === voz.canalId)
  return (
    <div className="barra-voz">
      <button className="barra-voz-info" onClick={() => navegar(`/s/${voz.servidorId}/${voz.canalId}`)}>
        <strong className={voz.estado === 'conectada' ? 'ok' : ''}>
          <Icone nome="sinal" tamanho={16} />
          {voz.estado === 'conectada' ? 'Voz conectada' : 'Conectando…'}
        </strong>
        <small>
          {canal?.nome || 'Sala de voz'} / {v?.servidor.nome}
        </small>
      </button>
      <Controles perm={BigInt(canal?.permissoes || 0)} compacto />
    </div>
  )
}
