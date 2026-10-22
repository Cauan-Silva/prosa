import { useEffect, useRef, useState } from 'react'
import Icone from './Icone.jsx'
import { Avatar } from './Azulejo.jsx'
import { nomeDe, useEstado } from '../servicos/estado.jsx'
import { alternarSilencioLocal, mudarVolume, useVoz } from '../servicos/voz.jsx'
import { usePreferencias } from '../servicos/preferencias.js'
import { P, tem } from '../servicos/permissoes.js'

export default function SalaVoz({ visao, canal }) {
  const { estado } = useEstado()
  const voz = useVoz()
  const sid = visao.servidor.id
  const pessoas = estado.voz[sid]?.[canal.id] || []
  const aqui = voz.canalId === canal.id
  const perm = BigInt(canal.permissoes || 0)

  if (!aqui) {
    return (
      <div className="sala-voz fora">
        <Icone nome="voz" tamanho={40} />
        <h2>{canal.nome}</h2>
        <p>
          {pessoas.length === 0
            ? 'Ninguém na sala agora.'
            : `${pessoas.map((p) => p.nome).join(', ')} ${pessoas.length === 1 ? 'está' : 'estão'} aqui.`}
        </p>
        {voz.estado === 'erro' && voz.erro && <p className="erro">{voz.erro}</p>}
        {tem(perm, P.ConectarVoz) ? (
          <button className="botao primario grande" onClick={() => voz.entrar(sid, canal.id)} disabled={voz.estado === 'conectando'}>
            {voz.estado === 'conectando' && voz.canalId === canal.id ? 'Entrando…' : voz.canalId ? 'Trocar para esta sala' : 'Entrar na sala'}
          </button>
        ) : (
          <p className="dica">Você não tem permissão para entrar nesta sala.</p>
        )}
      </div>
    )
  }

  return <Palco visao={visao} canal={canal} pessoas={pessoas} />
}

function Palco({ visao, canal, pessoas }) {
  const { estado } = useEstado()
  const prefs = usePreferencias()
  const voz = useVoz()
  const [foco, setFoco] = useState(null) // "id:tipo"
  const eu = estado.eu.id
  const membros = estado.membros[visao.servidor.id]
  const perm = BigInt(canal.permissoes || 0)
  const moderador = tem(perm, P.SilenciarNaVoz)

  // Cada pessoa vira um quadro; quem compartilha a tela ganha um quadro a mais.
  const quadros = []
  for (const p of pessoas) {
    const meu = p.usuarioId === eu
    const remoto = voz.remotos[p.usuarioId] || {}
    const camera = meu ? voz.local.camera : p.camera ? remoto.camera : null
    const tela = meu ? voz.local.tela : p.tela ? remoto.tela : null
    if (tela) quadros.push({ chave: `${p.usuarioId}:tela`, p, stream: tela, tela: true })
    quadros.push({ chave: `${p.usuarioId}:camera`, p, stream: camera })
  }
  const focado = quadros.find((q) => q.chave === foco) || quadros.find((q) => q.tela)
  const outros = focado ? quadros.filter((q) => q !== focado) : quadros

  return (
    <div className="palco">
      {voz.estado === 'conectando' && <p className="aviso-lista">Conectando à sala…</p>}
      {voz.erro && <p className="erro palco-erro">{voz.erro}</p>}
      {voz.semMicrofone && <p className="dica palco-erro">Sem microfone: você está só ouvindo.</p>}
      {prefs.voz.modo === 'ptt' && !voz.mudo && (
        <p className={`dica palco-erro ptt ${voz.pttAtivo ? 'ligado' : ''}`}>
          {voz.pttAtivo ? 'Falando…' : `Segure ${nomeTecla(prefs.voz.teclaPtt)} para falar`}
        </p>
      )}
      {voz.tela && !voz.telaComSom && (
        <p className="dica palco-erro">
          Sua tela está sem som. Para mandar o áudio, pare e compartilhe de novo marcando “Compartilhar áudio” (no Chrome ou Edge, ao escolher uma aba ou a tela inteira).
        </p>
      )}
      <div className={`quadros ${focado ? 'com-foco' : ''}`}>
        {focado && (
          <div className="quadro-foco">
            <Quadro
              q={focado}
              falando={falandoDe(voz, focado.p, eu)}
              nome={nomeDe(membros?.find((m) => m.usuarioId === focado.p.usuarioId)) || focado.p.nome}
              avatar={membros?.find((m) => m.usuarioId === focado.p.usuarioId)?.avatar}
              eu={eu}
              moderador={moderador && focado.p.usuarioId !== eu}
              aoModerar={voz.moderar}
              prefsVoz={prefs.voz}
              aoClicar={() => setFoco(null)}
            />
          </div>
        )}
        <div className="grade-quadros" data-qtd={Math.min(outros.length, 9)}>
          {outros.map((q) => (
            <Quadro
              key={q.chave}
              q={q}
              eu={eu}
              falando={falandoDe(voz, q.p, eu)}
              nome={nomeDe(membros?.find((m) => m.usuarioId === q.p.usuarioId)) || q.p.nome}
              avatar={membros?.find((m) => m.usuarioId === q.p.usuarioId)?.avatar}
              moderador={moderador && q.p.usuarioId !== eu}
              aoModerar={voz.moderar}
              prefsVoz={prefs.voz}
              aoClicar={() => (q.stream ? setFoco(q.chave) : null)}
            />
          ))}
        </div>
      </div>
      <Controles perm={perm} />
    </div>
  )
}

const falandoDe = (voz, p, eu) => !p.mudo && !p.silenciado && !!voz.falando[p.usuarioId === eu ? 'eu' : p.usuarioId]

function Quadro({ q, nome, avatar, falando, eu, moderador, aoModerar, prefsVoz, aoClicar }) {
  const { p } = q
  const [menu, setMenu] = useState(false)
  const meu = p.usuarioId === eu
  // o quadro da tela controla o som da tela; o da pessoa, a voz dela
  const chave = q.tela ? `t${p.usuarioId}` : String(p.usuarioId)
  const silenciadoPorMim = (prefsVoz?.silenciados || []).includes(chave)
  const volume = Math.round((prefsVoz?.volumes?.[chave] ?? 1) * 100)
  return (
    <div className={`quadro ${falando ? 'falando' : ''} ${q.stream ? 'com-video' : ''}`} onClick={aoClicar}>
      {q.stream ? <Video stream={q.stream} espelhar={p.usuarioId === eu && !q.tela} /> : <Avatar nome={nome} id={p.usuarioId} src={avatar} tamanho={72} />}
      <div className="quadro-rodape">
        <span>
          {nome}
          {q.tela && ' (tela)'}
        </span>
        {(p.mudo || p.silenciado) && <Icone nome="microfone-off" tamanho={16} />}
        {!meu && silenciadoPorMim && <Icone nome="voz-off" tamanho={16} />}
        {p.surdo && <Icone nome="fone-off" tamanho={16} />}
      </div>
      {p.silenciado && <span className="etiqueta perigo quadro-etiqueta">silenciado</span>}
      {!meu && (
        <div className="quadro-moderar" onClick={(e) => e.stopPropagation()}>
          <button
            className="icone-botao"
            onClick={() => setMenu((m) => !m)}
            aria-label={q.tela ? `Som da tela de ${nome}` : moderador ? `Moderar ${nome}` : `Volume de ${nome}`}
            aria-expanded={menu}
            title={q.tela ? 'Volume da tela' : 'Volume e opções'}
          >
            <Icone nome={moderador && !q.tela ? 'config' : 'volume'} tamanho={16} />
          </button>
          {menu && (
            <div className="menu menu-volume">
              <label className="volume-pessoa">
                <span>
                  {q.tela ? 'Volume da tela' : 'Volume'} <strong>{silenciadoPorMim ? 'mudo' : `${volume}%`}</strong>
                </span>
                <input
                  type="range"
                  min="0"
                  max="200"
                  step="5"
                  value={volume}
                  onChange={(e) => mudarVolume(chave, Number(e.target.value) / 100)}
                  onDoubleClick={() => mudarVolume(chave, 1)}
                  aria-label={`Volume de ${nome}${q.tela ? ' (tela)' : ''}`}
                />
              </label>
              <button onClick={() => alternarSilencioLocal(chave)}>
                {silenciadoPorMim ? 'Voltar a ouvir' : q.tela ? 'Silenciar a tela para mim' : 'Silenciar só para mim'}
              </button>
              {moderador && !q.tela && (
                <>
                  <button onClick={() => (aoModerar(p.usuarioId, p.silenciado ? 'liberar' : 'silenciar'), setMenu(false))}>
                    {p.silenciado ? 'Liberar a voz' : 'Silenciar para todos'}
                  </button>
                  <button className="perigo" onClick={() => (aoModerar(p.usuarioId, 'desconectar'), setMenu(false))}>
                    Tirar da sala
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function Video({ stream, espelhar }) {
  const ref = useRef(null)
  useEffect(() => {
    if (ref.current && ref.current.srcObject !== stream) ref.current.srcObject = stream
  }, [stream])
  return <video ref={ref} autoPlay playsInline muted className={espelhar ? 'espelho' : ''} />
}

export function Controles({ perm, compacto }) {
  const voz = useVoz()
  const podeFalar = tem(perm, P.Falar)
  const podeVideo = tem(perm, P.Video)
  return (
    <div className={`controles-voz ${compacto ? 'compacto' : ''}`} role="toolbar" aria-label="Controles da voz">
      <button
        className={`controle ${voz.mudo || !podeFalar || voz.semMicrofone ? 'desligado' : ''}`}
        onClick={() => voz.aplicar({ mudo: !voz.mudo })}
        disabled={!podeFalar || voz.semMicrofone}
        aria-pressed={!voz.mudo}
        title={voz.mudo ? 'Ligar microfone' : 'Desligar microfone'}
        aria-label={voz.mudo ? 'Ligar microfone' : 'Desligar microfone'}
      >
        <Icone nome={voz.mudo || !podeFalar ? 'microfone-off' : 'microfone'} />
      </button>
      <button
        className={`controle ${voz.surdo ? 'desligado' : ''}`}
        onClick={() => voz.aplicar({ surdo: !voz.surdo })}
        aria-pressed={voz.surdo}
        title={voz.surdo ? 'Voltar a ouvir' : 'Parar de ouvir'}
        aria-label={voz.surdo ? 'Voltar a ouvir' : 'Parar de ouvir'}
      >
        <Icone nome={voz.surdo ? 'fone-off' : 'fone'} />
      </button>
      {!compacto && (
        <>
          <button
            className={`controle ${voz.camera ? 'ligado' : ''}`}
            onClick={voz.alternarCamera}
            disabled={!podeVideo}
            aria-pressed={voz.camera}
            title={voz.camera ? 'Desligar câmera' : 'Ligar câmera'}
            aria-label={voz.camera ? 'Desligar câmera' : 'Ligar câmera'}
          >
            <Icone nome={voz.camera ? 'camera' : 'camera-off'} />
          </button>
          <button
            className={`controle ${voz.tela ? 'ligado' : ''}`}
            onClick={voz.alternarTela}
            disabled={!podeVideo || !navigator.mediaDevices?.getDisplayMedia}
            aria-pressed={voz.tela}
            title={voz.tela ? 'Parar de compartilhar' : 'Compartilhar a tela'}
            aria-label={voz.tela ? 'Parar de compartilhar' : 'Compartilhar a tela'}
          >
            <Icone nome="tela" />
          </button>
        </>
      )}
      {!compacto && voz.tela && (
        <div className="modo-tela" role="radiogroup" aria-label="Prioridade da tela">
          {[
            ['nitido', 'Nítido', 'Mais resolução, 10 quadros por segundo (texto, código, slides)'],
            ['fluido', 'Fluido', '30 quadros por segundo (o equilíbrio)'],
            ['fps60', '60 fps', '60 quadros por segundo; baixa a resolução se precisar (jogos, vídeos). Gasta mais internet'],
          ].map(([v, r, dica]) => (
            <button key={v} role="radio" aria-checked={voz.modoTela === v} title={dica} onClick={() => voz.mudarModoTela(v)}>
              {r}
            </button>
          ))}
        </div>
      )}
      <button className="controle sair" onClick={voz.sair} title="Sair da sala" aria-label="Sair da sala">
        <Icone nome="desligar" />
      </button>
    </div>
  )
}

export function nomeTecla(codigo) {
  if (!codigo) return 'uma tecla'
  if (codigo.startsWith('Key')) return codigo.slice(3)
  if (codigo.startsWith('Digit')) return codigo.slice(5)
  const nomes = { Space: 'Espaço', ShiftLeft: 'Shift', ShiftRight: 'Shift', ControlLeft: 'Ctrl', ControlRight: 'Ctrl', AltLeft: 'Alt', AltRight: 'Alt Gr', Backquote: "'", CapsLock: 'Caps Lock', Tab: 'Tab' }
  return nomes[codigo] || codigo.replace(/^Numpad/, 'Num ')
}
