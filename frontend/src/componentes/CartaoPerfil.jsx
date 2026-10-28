import { Avatar } from './Azulejo.jsx'
import { Efeito, NomeEstilizado } from './Visual.jsx'
import { corHex, formatarDia } from '../servicos/formato.js'
import { hex, temaEscuro } from '../servicos/visual.js'

const STATUS = { online: 'Online', ausente: 'Ausente', ocupado: 'Não perturbe', invisivel: 'Invisível' }

/** O cartão de perfil (banner, foto, nome, status e "sobre mim"), igual para todo mundo. */
export default function CartaoPerfil({ perfil, online, apelido, desde, children }) {
  const cor = corHex(perfil.corPerfil) || 'var(--cobalto)'
  const v = perfil.visual || {}
  const tema = v.tema?.length === 2 ? v.tema : null
  const estilo = { '--cor-perfil': tema ? hex(tema[0]) : cor }
  if (tema) {
    estilo['--tema1'] = hex(tema[0])
    estilo['--tema2'] = hex(tema[1])
  }
  const classeTema = tema ? `com-tema ${temaEscuro(tema) ? 'tema-escuro' : 'tema-claro'}` : ''
  return (
    <div className={`cartao-perfil ${classeTema}`} style={estilo}>
      <div className="perfil-banner" style={perfil.banner ? { backgroundImage: `url(${perfil.banner})` } : undefined} />
      <div className="perfil-cabeca">
        <span className="perfil-foto">
          <Avatar nome={perfil.nomeExibicao} id={perfil.usuarioId ?? perfil.id} src={perfil.avatar} moldura={v.moldura} tamanho={88} online={online} status={perfil.status} />
        </span>
        {(perfil.statusTexto || perfil.statusEmoji) && (
          <span className="perfil-balao">
            {perfil.statusEmoji} {perfil.statusTexto}
          </span>
        )}
      </div>
      <div className="perfil-corpo">
        <NomeEstilizado como="h3" estilo={v.nome} tema={v.tema}>
          {apelido || perfil.nomeExibicao}
        </NomeEstilizado>
        <p className="perfil-usuario">
          @{perfil.nomeUsuario}
          {perfil.pronomes && <span> — {perfil.pronomes}</span>}
        </p>
        {online !== undefined && (
          <p className="perfil-status">
            <span className={`ponto-online inline ${!online ? 'off' : perfil.status || 'online'}`} /> {online ? STATUS[perfil.status] || 'Online' : 'Offline'}
          </p>
        )}
        {perfil.bio && (
          <section>
            <h4>Sobre mim</h4>
            <p className="perfil-bio">{perfil.bio}</p>
          </section>
        )}
        {(perfil.criadoEm || desde) && (
          <section className="perfil-datas">
            {perfil.criadoEm && (
              <div>
                <h4>No Prosa desde</h4>
                <p>{formatarDia(perfil.criadoEm).toLowerCase()}</p>
              </div>
            )}
            {desde && (
              <div>
                <h4>Neste servidor desde</h4>
                <p>{formatarDia(desde).toLowerCase()}</p>
              </div>
            )}
          </section>
        )}
        {children}
      </div>
      <Efeito id={v.efeito} />
    </div>
  )
}
