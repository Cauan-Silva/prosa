import { useEffect, useMemo, useState } from 'react'
import { Azulejo } from '../componentes/Azulejo.jsx'
import Canais from '../componentes/Canais.jsx'
import Conversa from '../componentes/Conversa.jsx'
import Membros from '../componentes/Membros.jsx'
import NovoServidor from '../componentes/NovoServidor.jsx'
import ConfigServidor from '../componentes/ConfigServidor.jsx'
import ConfigCanal from '../componentes/ConfigCanal.jsx'
import Convidar from '../componentes/Convidar.jsx'
import Configuracoes from '../componentes/Configuracoes.jsx'
import CartaoMembro from '../componentes/CartaoMembro.jsx'
import AceitarConvite from '../componentes/AceitarConvite.jsx'
import Icone from '../componentes/Icone.jsx'
import { useEstado } from '../servicos/estado.jsx'
import { navegar } from '../servicos/rota.jsx'
import { api } from '../servicos/api.js'
import { useNovaVersao } from '../servicos/versao.js'

const CHAVE_CANAIS = 'prosa.ultimosCanais'
function lerUltimos() {
  try {
    return JSON.parse(localStorage.getItem(CHAVE_CANAIS)) || {}
  } catch {
    return {}
  }
}
function guardarUltimo(servidor, canal) {
  try {
    localStorage.setItem(CHAVE_CANAIS, JSON.stringify({ ...lerUltimos(), [servidor]: canal }))
  } catch {
    // sem armazenamento
  }
}

export default function Aplicativo({ caminho }) {
  const { estado, despachar } = useEstado()
  const [modal, setModal] = useState(null) // { tipo, ... }
  const [mostrarMembros, setMostrarMembros] = useState(() => window.innerWidth > 1100)
  const [painel, setPainel] = useState('conversa') // no celular: canais | conversa | membros
  const [perfilAberto, setPerfilAberto] = useState(null)

  // Clique em nome ou foto de alguém (em qualquer lugar) abre o cartão de perfil.
  useEffect(() => {
    const abrir = (e) => setPerfilAberto(e.detail.usuarioId)
    window.addEventListener('prosa:perfil', abrir)
    return () => window.removeEventListener('prosa:perfil', abrir)
  }, [])

  const [, sid, cid] = caminho.match(/^\/s\/(\d+)(?:\/(\d+))?/) || []
  const servidorId = Number(sid) || null
  const canalId = Number(cid) || null
  const convite = caminho.match(/^\/convite\/([\w-]+)/)?.[1]
  const visao = servidorId ? estado.servidores[servidorId] : null
  const ordem = useMemo(
    () => Object.values(estado.servidores).sort((a, b) => a.servidor.id - b.servidor.id),
    [estado.servidores],
  )

  // Escolhe para onde ir quando o endereço não diz (ou aponta para algo que sumiu).
  useEffect(() => {
    if (!estado.pronto || convite) return
    if (servidorId && !visao) {
      navegar('/', { substituir: true })
      return
    }
    if (!servidorId) {
      if (caminho === '/' && ordem.length) {
        const primeiro = ordem[0].servidor.id
        navegar(`/s/${primeiro}`, { substituir: true })
      }
      return
    }
    const textos = visao.canais.filter((c) => c.tipo === 'texto')
    if (!canalId || !visao.canais.some((c) => c.id === canalId)) {
      const ultimo = lerUltimos()[servidorId]
      const destino = textos.find((c) => c.id === ultimo) || textos[0]
      if (destino) navegar(`/s/${servidorId}/${destino.id}`, { substituir: true })
    }
  }, [estado.pronto, servidorId, canalId, visao, ordem, caminho, convite])

  useEffect(() => {
    if (servidorId && canalId) guardarUltimo(servidorId, canalId)
  }, [servidorId, canalId])

  // Membros do servidor aberto (para nomes, menções e a lista lateral).
  useEffect(() => {
    if (!servidorId || !visao || estado.membros[servidorId]) return
    api.membros(servidorId).then((lista) => despachar({ tipo: 'membros', s: servidorId, lista })).catch(() => {})
  }, [servidorId, visao, estado.membros, despachar])

  // Título da aba com não lidas
  const naoLidos = ordem.reduce(
    (t, v) => t + v.canais.filter((c) => c.tipo === 'texto' && (c.ultimaMensagemId || 0) > (c.ultimaLida || 0)).length,
    0,
  )
  useEffect(() => {
    const canal = visao?.canais.find((c) => c.id === canalId)
    document.title = `${naoLidos ? `(${naoLidos}) ` : ''}${canal ? `#${canal.nome} · ` : ''}Prosa`
  }, [naoLidos, visao, canalId])

  const canal = visao?.canais.find((c) => c.id === canalId)
  const abrir = (tipo, extra = {}) => setModal({ tipo, ...extra })
  const fechar = () => setModal(null)

  const novaVersao = useNovaVersao()

  if (!estado.pronto) {
    return (
      <div className="carregando-tela">
        <p>{estado.conexao === 'reconectando' ? 'Sem conexão. Tentando de novo…' : 'Abrindo o Prosa…'}</p>
      </div>
    )
  }

  return (
    <div className={`app painel-${visao ? painel : 'canais'} ${mostrarMembros && visao ? 'com-membros' : ''}`}>
      {estado.conexao !== 'online' ? (
        <div className="faixa-conexao" role="status">
          Sem conexão. Tentando reconectar…
        </div>
      ) : (
        novaVersao && (
          <div className="faixa-conexao faixa-versao" role="status">
            Saiu uma versão nova do Prosa.
            <button onClick={() => location.reload()}>Atualizar agora</button>
          </div>
        )
      )}

      <nav className="trilho" aria-label="Servidores">
        <button className="trilho-logo" onClick={() => navegar('/')} title="Prosa" aria-label="Prosa, início">
          <img src="/icone-64.png" alt="" width="48" height="48" />
        </button>
        <span className="trilho-divisor" aria-hidden="true" />
        {ordem.map((v) => {
          const id = v.servidor.id
          const naoLido = v.canais.some((c) => c.tipo === 'texto' && (c.ultimaMensagemId || 0) > (c.ultimaLida || 0))
          const mencoes = v.canais.reduce((t, c) => t + (c.mencoesNaoLidas || 0), 0)
          return (
            <button
              key={id}
              className={`trilho-item ${id === servidorId ? 'ativo' : ''} ${naoLido ? 'nao-lido' : ''}`}
              onClick={() => {
                navegar(`/s/${id}`)
                setPainel('canais')
              }}
              title={v.servidor.nome}
              aria-label={`${v.servidor.nome}${mencoes ? `, ${mencoes} menções` : naoLido ? ', mensagens novas' : ''}`}
              aria-current={id === servidorId ? 'page' : undefined}
            >
              <Azulejo nome={v.servidor.nome} src={v.servidor.icone} tamanho={48} />
              {mencoes > 0 && <span className="selo">{mencoes > 99 ? '99+' : mencoes}</span>}
            </button>
          )
        })}
        <button className="trilho-item trilho-acao" onClick={() => abrir('novo')} title="Criar ou entrar num servidor" aria-label="Criar ou entrar num servidor">
          <Icone nome="mais" tamanho={24} />
        </button>
      </nav>

      {visao ? (
        <>
          <Canais
            visao={visao}
            canalId={canalId}
            aoAbrir={abrir}
            aoEscolher={(id) => {
              navegar(`/s/${servidorId}/${id}`)
              setPainel('conversa')
            }}
          />
          {canal ? (
            <Conversa
              key={canal.id}
              visao={visao}
              canal={canal}
              mostrarMembros={mostrarMembros}
              aoAlternarMembros={() => {
                setMostrarMembros((m) => !m)
                setPainel((p) => (p === 'membros' ? 'conversa' : 'membros'))
              }}
              aoVoltar={() => setPainel('canais')}
            />
          ) : (
            <main className="conversa vazia">
              <p>Escolha um canal na lista.</p>
            </main>
          )}
          {mostrarMembros && <Membros visao={visao} aoAbrir={abrir} />}
        </>
      ) : (
        <main className="boas-vindas">
          <img className="boas-vindas-capa" src="/capa-pequena.webp" alt="Prosa" width="280" height="280" />
          <h2>Bem-vindo ao Prosa, {estado.eu.nomeExibicao}</h2>
          <p>Crie um servidor para sua turma ou entre com o link de convite que alguém mandou.</p>
          <button className="botao primario grande" onClick={() => abrir('novo')}>
            Criar ou entrar num servidor
          </button>
        </main>
      )}

      {modal?.tipo === 'novo' && <NovoServidor aoFechar={fechar} />}
      {modal?.tipo === 'config-servidor' && visao && <ConfigServidor visao={visao} aba={modal.aba} aoFechar={fechar} />}
      {modal?.tipo === 'config-canal' && visao && (
        <ConfigCanal visao={visao} canal={visao.canais.find((c) => c.id === modal.canalId)} aoFechar={fechar} />
      )}
      {modal?.tipo === 'novo-canal' && visao && <ConfigCanal visao={visao} categoriaId={modal.categoriaId} tipoInicial={modal.tipoCanal} aoFechar={fechar} />}
      {modal?.tipo === 'convidar' && visao && <Convidar visao={visao} canalId={modal.canalId || canalId} aoFechar={fechar} />}
      {modal?.tipo === 'config' && <Configuracoes aba={modal.aba} aoFechar={fechar} />}
      {perfilAberto && <CartaoMembro visao={visao} usuarioId={perfilAberto} aoFechar={() => setPerfilAberto(null)} />}
      {convite && <AceitarConvite codigo={convite} aoFechar={() => navegar('/', { substituir: true })} />}
    </div>
  )
}
