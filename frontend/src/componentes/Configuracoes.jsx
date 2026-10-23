import { useEffect, useRef, useState } from 'react'
import { recortar } from '../servicos/imagem.js'
import Modal from './Modal.jsx'
import Icone from './Icone.jsx'
import CartaoPerfil from './CartaoPerfil.jsx'
import { nomeTecla } from './SalaVoz.jsx'
import { RAPIDOS } from './Mensagem.jsx'
import { api, enviarImagemPerfil } from '../servicos/api.js'
import { useEstado } from '../servicos/estado.jsx'
import { useSessao } from '../servicos/sessao.jsx'
import { mudar, usePreferencias } from '../servicos/preferencias.js'
import { FONTES, TEMAS, coresDe } from '../servicos/aparencia.js'
import { configSons, mudarSons, tocar } from '../servicos/sons.js'
import { Processador, abrirMicrofone } from '../servicos/microfone.js'
import { corHex } from '../servicos/formato.js'
import { LINK_DOWNLOAD, inicioAutomatico, noDesktop, versaoDesktop } from '../servicos/desktop.js'

const ABAS = [
  ['conta', 'Minha conta', 'pessoas'],
  ['perfil', 'Perfil', 'editar'],
  ['aparencia', 'Aparência', 'sol'],
  ['voz', 'Voz e vídeo', 'microfone'],
  ['notificacoes', 'Notificações e sons', 'sinal'],
  ['acessibilidade', 'Acessibilidade', 'bussola'],
  ['app', 'Instalar o app', 'tela'],
]

export default function Configuracoes({ aba: inicial, aoFechar }) {
  const [aba, setAba] = useState(inicial || 'perfil')
  const { sair } = useSessao()
  return (
    <Modal titulo="Configurações" aoFechar={aoFechar} largura="tela">
      <div className="configuracoes">
        <nav className="config-abas" role="tablist" aria-orientation="vertical">
          {ABAS.map(([id, nome, icone]) => (
            <button key={id} role="tab" aria-selected={aba === id} onClick={() => setAba(id)}>
              <Icone nome={icone} tamanho={18} /> {nome}
            </button>
          ))}
          <hr />
          <button className="perigo" onClick={sair}>
            <Icone nome="sair" tamanho={18} /> Sair da conta
          </button>
        </nav>
        <div className="config-conteudo">
          {aba === 'conta' && <Conta />}
          {aba === 'perfil' && <Perfil />}
          {aba === 'aparencia' && <Aparencia />}
          {aba === 'voz' && <Voz />}
          {aba === 'notificacoes' && <Notificacoes />}
          {aba === 'acessibilidade' && <Acessibilidade />}
          {aba === 'app' && (
            <>
              <ProgramaWindows />
              {!noDesktop && <Instalar />}
            </>
          )}
        </div>
      </div>
    </Modal>
  )
}

function Interruptor({ rotulo, dica, ligado, aoMudar, desativado }) {
  return (
    <label className="interruptor">
      <span>
        {rotulo}
        {dica && <small>{dica}</small>}
      </span>
      <input type="checkbox" role="switch" checked={!!ligado} disabled={desativado} onChange={(e) => aoMudar(e.target.checked)} />
    </label>
  )
}

function Escolha({ opcoes, valor, aoMudar, rotulo }) {
  return (
    <div className="escolha" role="radiogroup" aria-label={rotulo}>
      {opcoes.map(([v, r]) => (
        <button key={v} role="radio" aria-checked={valor === v} onClick={() => aoMudar(v)}>
          {r}
        </button>
      ))}
    </div>
  )
}

// ---------- conta ----------

function Conta() {
  const { estado, despachar } = useEstado()
  const { setUsuario } = useSessao()
  const [nome, setNome] = useState(estado.eu.nomeExibicao)
  const [msg, setMsg] = useState('')
  async function salvar(e) {
    e.preventDefault()
    try {
      const u = await api.alterarPerfil({ nomeExibicao: nome })
      setUsuario(u)
      despachar({ tipo: 'eu', usuario: u })
      setMsg('Nome salvo.')
    } catch (err) {
      setMsg(err.message)
    }
  }
  return (
    <section>
      <h2>Minha conta</h2>
      <form className="formulario estreito" onSubmit={salvar}>
        <label>
          Nome de exibição
          <input value={nome} onChange={(e) => setNome(e.target.value)} maxLength={40} required />
          <small>É como você aparece para todo mundo. Em cada servidor dá para ter um apelido diferente.</small>
        </label>
        <label>
          Nome de usuário
          <input value={`@${estado.eu.nomeUsuario}`} readOnly />
        </label>
        <label>
          E-mail
          <input value={estado.eu.email || ''} readOnly />
          <small>Só você vê o seu e-mail.</small>
        </label>
        <button className="botao primario" disabled={nome.trim() === estado.eu.nomeExibicao}>
          Salvar
        </button>
        {msg && <p role="status">{msg}</p>}
      </form>
    </section>
  )
}

// ---------- perfil ----------

const CORES_PERFIL = [0, 0x1f3fa6, 0x7c5cff, 0xd63a7a, 0xe5484d, 0xf2994a, 0xe8b420, 0x3fbf7f, 0x14635a, 0x22b8cf, 0x5b6478]
const STATUS = [
  ['online', 'Online'],
  ['ausente', 'Ausente'],
  ['ocupado', 'Não perturbe'],
  ['invisivel', 'Invisível'],
]

function Perfil() {
  const { estado, despachar } = useEstado()
  const eu = estado.eu
  const [campos, setCampos] = useState({
    bio: eu.bio || '', pronomes: eu.pronomes || '', corPerfil: eu.corPerfil || 0,
    status: eu.status || 'online', statusTexto: eu.statusTexto || '', statusEmoji: eu.statusEmoji || '',
  })
  const [msg, setMsg] = useState('')
  const [enviando, setEnviando] = useState('')
  const mudou = Object.entries(campos).some(([k, v]) => (eu[k] ?? '') !== v && !(k === 'corPerfil' && !eu[k] && !v))
  const mudarCampo = (k) => (e) => setCampos({ ...campos, [k]: e.target.value })

  async function salvar() {
    setMsg('')
    try {
      despachar({ tipo: 'eu', usuario: await api.alterarPerfil(campos) })
      setMsg('Perfil salvo.')
    } catch (e) {
      setMsg(e.message)
    }
  }

  async function imagem(campo, arquivo) {
    if (!arquivo) return
    setMsg('')
    setEnviando(campo)
    try {
      const blob = campo === 'avatar' ? await recortar(arquivo, 256, 256) : await recortar(arquivo, 960, 360)
      despachar({ tipo: 'eu', usuario: await enviarImagemPerfil(campo, blob) })
    } catch (e) {
      setMsg(e.message || 'Não foi possível usar essa imagem.')
    } finally {
      setEnviando('')
    }
  }

  async function tirar(campo) {
    try {
      despachar({ tipo: 'eu', usuario: await api.apagarImagemPerfil(campo) })
    } catch (e) {
      setMsg(e.message)
    }
  }

  return (
    <section>
      <h2>Perfil</h2>
      <p className="dica">É assim que as pessoas veem você quando clicam no seu nome.</p>
      <div className="perfil-edicao">
        <div className="formulario">
          <div className="linha-botoes">
            <label className="botao">
              {enviando === 'avatar' ? 'Enviando…' : 'Trocar foto'}
              <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => imagem('avatar', e.target.files[0])} />
            </label>
            {eu.avatar && (
              <button className="botao" onClick={() => tirar('avatar')}>
                Tirar foto
              </button>
            )}
            <label className="botao">
              {enviando === 'banner' ? 'Enviando…' : 'Trocar banner'}
              <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => imagem('banner', e.target.files[0])} />
            </label>
            {eu.banner && (
              <button className="botao" onClick={() => tirar('banner')}>
                Tirar banner
              </button>
            )}
          </div>
          <fieldset className="cores">
            <legend>Cor do perfil (aparece no banner quando não há imagem)</legend>
            {CORES_PERFIL.map((c) => (
              <button
                key={c}
                type="button"
                className={`cor ${campos.corPerfil === c ? 'escolhida' : ''}`}
                style={{ background: corHex(c) || 'var(--cobalto)' }}
                onClick={() => setCampos({ ...campos, corPerfil: c })}
                aria-label={c ? corHex(c) : 'Cor do tema'}
                aria-pressed={campos.corPerfil === c}
              >
                {c === 0 && 'A'}
              </button>
            ))}
            <input type="color" value={corHex(campos.corPerfil) || '#1f3fa6'} onChange={(e) => setCampos({ ...campos, corPerfil: parseInt(e.target.value.slice(1), 16) })} aria-label="Outra cor" />
          </fieldset>
          <label>
            Pronomes
            <input value={campos.pronomes} onChange={mudarCampo('pronomes')} maxLength={40} placeholder="ex.: ele/dele" />
          </label>
          <label>
            Sobre mim
            <textarea value={campos.bio} onChange={mudarCampo('bio')} maxLength={400} rows={4} placeholder="Conte um pouco sobre você" />
            <small>{400 - campos.bio.length} caracteres restantes</small>
          </label>
          <div>
            <p className="rotulo">Status</p>
            <Escolha rotulo="Status" opcoes={STATUS} valor={campos.status} aoMudar={(v) => setCampos({ ...campos, status: v })} />
          </div>
          <label>
            Status personalizado
            <div className="status-personalizado">
              <select value={campos.statusEmoji} onChange={mudarCampo('statusEmoji')} aria-label="Emoji do status">
                <option value="">sem emoji</option>
                {['💬', '🎮', '📚', '🎧', '💻', '☕', '😴', '🏠', '🚗', '🎉', ...RAPIDOS].filter((x, i, l) => l.indexOf(x) === i).map((e) => (
                  <option key={e} value={e}>
                    {e}
                  </option>
                ))}
              </select>
              <input value={campos.statusTexto} onChange={mudarCampo('statusTexto')} maxLength={80} placeholder="ex.: jogando, estudando, no trabalho" />
            </div>
          </label>
          <div className="botoes">
            <button className="botao primario" onClick={salvar} disabled={!mudou}>
              Salvar perfil
            </button>
          </div>
          {msg && <p role="status">{msg}</p>}
        </div>
        <div className="perfil-previa">
          <p className="rotulo">Prévia</p>
          <CartaoPerfil perfil={{ ...eu, ...campos }} online />
        </div>
      </div>
    </section>
  )
}

// ---------- aparência ----------

const DESTAQUES = ['', '#1f3fa6', '#5b78f0', '#7c5cff', '#a855f7', '#d63a7a', '#e5484d', '#f2994a', '#e8b420', '#3fbf7f', '#22b8cf', '#14b8a6']

function Aparencia() {
  const { aparencia: ap } = usePreferencias()
  const m = (x) => mudar('aparencia', x)
  return (
    <section>
      <h2>Aparência</h2>
      <h3>Tema</h3>
      <div className="temas">
        <button className={`tema ${ap.tema === 'sistema' ? 'escolhido' : ''}`} onClick={() => m({ tema: 'sistema' })} aria-pressed={ap.tema === 'sistema'}>
          <span className="tema-previa dividida">
            <Previa cores={coresDe({ ...ap, tema: 'azulejo-claro' })} />
            <Previa cores={coresDe({ ...ap, tema: 'azulejo-escuro' })} />
          </span>
          Do sistema
        </button>
        {TEMAS.map((t) => (
          <button key={t.id} className={`tema ${ap.tema === t.id ? 'escolhido' : ''}`} onClick={() => m({ tema: t.id })} aria-pressed={ap.tema === t.id}>
            <span className="tema-previa">
              <Previa cores={coresDe({ ...ap, tema: t.id })} />
            </span>
            {t.nome}
          </button>
        ))}
      </div>

      <h3>Cor de destaque</h3>
      <p className="dica">Botões, links, seleção e menções. “Do tema” usa a cor original de cada tema.</p>
      <div className="cores">
        {DESTAQUES.map((c) => (
          <button
            key={c || 'tema'}
            className={`cor ${ap.destaque === c ? 'escolhida' : ''}`}
            style={{ background: c || 'linear-gradient(135deg, var(--anil), var(--ipe))' }}
            onClick={() => m({ destaque: c })}
            aria-label={c || 'Do tema'}
            aria-pressed={ap.destaque === c}
          />
        ))}
        <input type="color" value={ap.destaque || '#1f3fa6'} onChange={(e) => m({ destaque: e.target.value })} aria-label="Escolher outra cor" />
      </div>

      <h3>Texto</h3>
      <div className="formulario estreito">
        <label>
          Fonte
          <select value={ap.fonte} onChange={(e) => m({ fonte: e.target.value })}>
            {FONTES.map((f) => (
              <option key={f.id} value={f.id}>
                {f.nome}
              </option>
            ))}
          </select>
        </label>
        <label>
          Tamanho ({Math.round((ap.escala || 1) * 100)}%)
          <input type="range" min="0.85" max="1.3" step="0.05" value={ap.escala || 1} onChange={(e) => m({ escala: Number(e.target.value) })} />
        </label>
      </div>

      <h3>Layout</h3>
      <div className="formulario estreito">
        <div>
          <p className="rotulo">Mensagens</p>
          <Escolha rotulo="Densidade" opcoes={[['confortavel', 'Confortável'], ['compacta', 'Compacta']]} valor={ap.densidade} aoMudar={(v) => m({ densidade: v })} />
        </div>
        <div>
          <p className="rotulo">Cantos</p>
          <Escolha rotulo="Cantos" opcoes={[['arredondados', 'Arredondados'], ['retos', 'Retos']]} valor={ap.cantos} aoMudar={(v) => m({ cantos: v })} />
        </div>
      </div>
      <p className="dica">As configurações ficam salvas na sua conta e valem em qualquer navegador.</p>
    </section>
  )
}

function Previa({ cores }) {
  return (
    <span className="previa" style={{ background: cores.giz }}>
      <span style={{ background: cores.anil }} />
      <span style={{ background: cores.papel }}>
        <i style={{ background: cores.cobalto }} />
        <i style={{ background: cores.linha }} />
        <i style={{ background: cores.ipe, width: '40%' }} />
      </span>
    </span>
  )
}

// ---------- voz e vídeo ----------

function useDispositivos() {
  const [lista, setLista] = useState([])
  useEffect(() => {
    const ler = () => navigator.mediaDevices?.enumerateDevices().then(setLista).catch(() => {})
    ler()
    navigator.mediaDevices?.addEventListener?.('devicechange', ler)
    return () => navigator.mediaDevices?.removeEventListener?.('devicechange', ler)
  }, [])
  return [lista, () => navigator.mediaDevices.enumerateDevices().then(setLista)]
}

function Voz() {
  const { voz } = usePreferencias()
  const m = (x) => mudar('voz', x)
  const [dispositivos, recarregar] = useDispositivos()
  const [teste, setTeste] = useState(null) // { proc }
  const [nivel, setNivel] = useState(-100)
  const [capturando, setCapturando] = useState(false)
  const previaCamera = useRef(null)
  const [cameraLigada, setCameraLigada] = useState(false)
  const temSaida = 'setSinkId' in HTMLMediaElement.prototype
  const semNomes = dispositivos.length > 0 && dispositivos.every((d) => !d.label)

  const mics = dispositivos.filter((d) => d.kind === 'audioinput')
  const saidas = dispositivos.filter((d) => d.kind === 'audiooutput')
  const cameras = dispositivos.filter((d) => d.kind === 'videoinput')

  // teste de microfone: mostra o volume e onde está a sensibilidade do portão
  useEffect(() => {
    if (!teste) return
    teste.proc.setOpcoes({ ...voz, modo: 'voz' })
    const t = setInterval(() => setNivel(teste.proc.nivel()), 60)
    return () => clearInterval(t)
  }, [teste, voz])
  useEffect(() => () => teste?.proc.destruir(), [teste])

  async function testar() {
    if (teste) {
      teste.audio.pause()
      setTeste(null)
      return
    }
    try {
      const proc = new Processador(await abrirMicrofone(voz), { ...voz, modo: 'voz' })
      const audio = new Audio()
      audio.srcObject = proc.stream // escuta a si mesmo (use fone para não dar microfonia)
      if (audio.setSinkId && voz.saida) await audio.setSinkId(voz.saida).catch(() => {})
      audio.play().catch(() => {})
      setTeste({ proc, audio })
      recarregar()
    } catch {
      alert('Não foi possível abrir o microfone. Confira a permissão do navegador.')
    }
  }

  useEffect(() => {
    if (!cameraLigada) return
    let stream
    navigator.mediaDevices
      .getUserMedia({ video: { deviceId: voz.camera ? { exact: voz.camera } : undefined, width: 640, height: 360 } })
      .then((s) => {
        stream = s
        if (previaCamera.current) previaCamera.current.srcObject = s
        recarregar()
      })
      .catch(() => setCameraLigada(false))
    return () => stream?.getTracks().forEach((t) => t.stop())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cameraLigada, voz.camera])

  useEffect(() => {
    if (!capturando) return
    const tecla = (e) => {
      e.preventDefault()
      if (e.code !== 'Escape') m({ teclaPtt: e.code })
      setCapturando(false)
    }
    window.addEventListener('keydown', tecla, { once: true, capture: true })
    return () => window.removeEventListener('keydown', tecla, { capture: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [capturando])

  const pos = (db) => `${Math.min(100, Math.max(0, ((db + 100) / 100) * 100))}%`

  return (
    <section>
      <h2>Voz e vídeo</h2>
      {semNomes && (
        <p className="dica">
          O navegador só mostra o nome dos aparelhos depois que você permite o uso. Clique em “Testar microfone” uma vez.
        </p>
      )}
      <div className="formulario estreito">
        <label>
          Microfone
          <select value={voz.microfone} onChange={(e) => m({ microfone: e.target.value })}>
            <option value="">Padrão do sistema</option>
            {mics.filter((d) => d.deviceId && d.deviceId !== 'default').map((d, i) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || `Microfone ${i + 1}`}
              </option>
            ))}
          </select>
        </label>
        {temSaida && (
          <label>
            Saída de som
            <select value={voz.saida} onChange={(e) => m({ saida: e.target.value })}>
              <option value="">Padrão do sistema</option>
              {saidas.filter((d) => d.deviceId && d.deviceId !== 'default').map((d, i) => (
                <option key={d.deviceId} value={d.deviceId}>
                  {d.label || `Saída ${i + 1}`}
                </option>
              ))}
            </select>
          </label>
        )}
        <label>
          Volume das outras pessoas ({Math.round((voz.volumeSaida ?? 1) * 100)}%)
          <input type="range" min="0" max="1" step="0.05" value={voz.volumeSaida ?? 1} onChange={(e) => m({ volumeSaida: Number(e.target.value) })} />
        </label>
      </div>

      <h3>Testar</h3>
      <div className="medidor-microfone">
        <div className="medidor" aria-hidden="true">
          <span className="medidor-nivel" style={{ width: teste ? pos(nivel) : '0%' }} />
          {voz.filtroRuido && <span className="medidor-limite" style={{ left: pos(voz.sensibilidade) }} />}
        </div>
        <button className="botao" onClick={testar}>
          {teste ? 'Parar teste' : 'Testar microfone'}
        </button>
      </div>
      <p className="dica">No teste você ouve a própria voz já com os filtros. Use fone de ouvido.</p>

      <h3>Ruído e eco</h3>
      <div className="lista-opcoes">
        <Interruptor
          rotulo="Supressão de ruído com IA (RNNoise)"
          dica="Uma rede neural tira teclado, ventilador, latido e barulho de fundo, deixando só a voz. Usa um pouco mais de processador; quando ligada, a supressão do navegador fica desligada."
          ligado={!!voz.supressaoIA}
          aoMudar={(v) => m({ supressaoIA: v })}
        />
        <Interruptor
          rotulo="Supressão de ruído do navegador"
          dica={voz.supressaoIA ? 'Desligada enquanto a supressão com IA estiver ligada' : 'Tira ventilador, chiado e barulho constante da sua voz (mais leve, menos forte que a IA)'}
          ligado={voz.supressaoRuido && !voz.supressaoIA}
          desativado={!!voz.supressaoIA}
          aoMudar={(v) => m({ supressaoRuido: v })}
        />
        <Interruptor
          rotulo="Cancelamento de eco"
          dica="Evita que o som das outras pessoas volte pelo seu microfone"
          ligado={voz.cancelamentoEco}
          aoMudar={(v) => m({ cancelamentoEco: v })}
        />
        <Interruptor
          rotulo="Ganho automático"
          dica="Ajusta o volume do microfone sozinho"
          ligado={voz.ganhoAutomatico}
          aoMudar={(v) => m({ ganhoAutomatico: v })}
        />
        <Interruptor
          rotulo="Filtro de ruído (portão)"
          dica="Corta todo som abaixo da sensibilidade: teclado, respiração e barulho entre as falas somem"
          ligado={voz.filtroRuido}
          aoMudar={(v) => m({ filtroRuido: v })}
        />
      </div>
      {voz.filtroRuido && (
        <div className="formulario estreito">
          <label>
            Sensibilidade ({voz.sensibilidade} dB)
            <input type="range" min="-80" max="-20" step="1" value={voz.sensibilidade} onChange={(e) => m({ sensibilidade: Number(e.target.value) })} />
            <small>Teste o microfone e deixe a marca amarela logo acima do barulho de fundo, e abaixo da sua voz.</small>
          </label>
        </div>
      )}

      <h3>Como o microfone abre</h3>
      <Escolha
        rotulo="Modo do microfone"
        opcoes={[['voz', 'Quando eu falar'], ['ptt', 'Pressionar para falar']]}
        valor={voz.modo}
        aoMudar={(v) => m({ modo: v })}
      />
      {voz.modo === 'ptt' && (
        <div className="linha-botoes ptt-tecla">
          <span>
            Tecla: <kbd>{nomeTecla(voz.teclaPtt)}</kbd>
          </span>
          <button className="botao" onClick={() => setCapturando(true)}>
            {capturando ? 'Aperte a tecla… (Esc cancela)' : 'Trocar tecla'}
          </button>
          <p className="dica">Funciona com o Prosa aberto e em foco (no navegador não dá para ouvir teclas fora da janela).</p>
        </div>
      )}

      <h3>Câmera</h3>
      <div className="formulario estreito">
        <label>
          Câmera
          <select value={voz.camera} onChange={(e) => m({ camera: e.target.value })}>
            <option value="">Padrão do sistema</option>
            {cameras.filter((d) => d.deviceId).map((d, i) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || `Câmera ${i + 1}`}
              </option>
            ))}
          </select>
        </label>
        <button className="botao" onClick={() => setCameraLigada((c) => !c)}>
          {cameraLigada ? 'Desligar prévia' : 'Ver prévia da câmera'}
        </button>
        {cameraLigada && <video ref={previaCamera} className="previa-camera" autoPlay playsInline muted />}
      </div>
    </section>
  )
}

// ---------- notificações e sons ----------

function Notificacoes() {
  const { notificacoes: n } = usePreferencias()
  const [sons, setSons] = useState(configSons)
  const [permissao, setPermissao] = useState(() => ('Notification' in window ? Notification.permission : 'indisponivel'))
  const mudarSom = (x) => {
    mudarSons(x)
    setSons(configSons())
  }
  async function ligarArea(v) {
    if (v && permissao !== 'granted') {
      const r = await Notification.requestPermission()
      setPermissao(r)
      if (r !== 'granted') return
    }
    mudar('notificacoes', { area: v })
  }
  return (
    <section>
      <h2>Notificações e sons</h2>
      <h3>Sons</h3>
      <div className="lista-opcoes">
        <Interruptor rotulo="Tocar sons" dica="Mensagens, menções, voz e botões" ligado={sons.ligado} aoMudar={(v) => mudarSom({ ligado: v })} />
      </div>
      <div className="formulario estreito">
        <label>
          Volume dos sons ({Math.round(sons.volume * 100)}%)
          <input type="range" min="0" max="1" step="0.05" value={sons.volume} disabled={!sons.ligado} onChange={(e) => mudarSom({ volume: Number(e.target.value) })} onPointerUp={() => tocar('mencao')} />
        </label>
      </div>
      <div className="linha-botoes">
        {[['mensagem', 'Mensagem'], ['mencao', 'Menção'], ['vozEntrar', 'Entrar na voz'], ['mudo', 'Microfone']].map(([s, r]) => (
          <button key={s} className="botao pequeno" onClick={() => tocar(s)} disabled={!sons.ligado}>
            Ouvir: {r}
          </button>
        ))}
      </div>

      <h3>Notificações do sistema</h3>
      {permissao === 'indisponivel' ? (
        <p className="dica">Este navegador não mostra notificações.</p>
      ) : (
        <div className="lista-opcoes">
          <Interruptor
            rotulo="Avisar quando o Prosa estiver em segundo plano"
            dica={permissao === 'denied' ? 'As notificações estão bloqueadas nas configurações do navegador para este site.' : 'Aparece no canto da tela, como as do WhatsApp'}
            ligado={n.area && permissao === 'granted'}
            desativado={permissao === 'denied'}
            aoMudar={ligarArea}
          />
          <Interruptor rotulo="Toda mensagem nova" dica="Desligado: só quando mencionarem você" ligado={n.todas} desativado={!n.area} aoMudar={(v) => mudar('notificacoes', { todas: v })} />
        </div>
      )}
      <p className="dica">Com o status “Não perturbe”, nenhum som de mensagem nem notificação aparece.</p>
    </section>
  )
}

// ---------- acessibilidade ----------

function Acessibilidade() {
  const { aparencia: ap } = usePreferencias()
  const m = (x) => mudar('aparencia', x)
  return (
    <section>
      <h2>Acessibilidade</h2>
      <div className="formulario estreito">
        <label>
          Tamanho do texto e da interface ({Math.round((ap.escala || 1) * 100)}%)
          <input type="range" min="0.85" max="1.3" step="0.05" value={ap.escala || 1} onChange={(e) => m({ escala: Number(e.target.value) })} />
        </label>
      </div>
      <div className="lista-opcoes">
        <Interruptor rotulo="Animações" dica="Desligue se movimento na tela incomoda" ligado={ap.animacoes !== false} aoMudar={(v) => m({ animacoes: v })} />
        <Interruptor rotulo="Sublinhar links" dica="Links ficam fáceis de ver sem depender da cor" ligado={ap.sublinharLinks} aoMudar={(v) => m({ sublinharLinks: v })} />
        <Interruptor rotulo="Alto contraste" dica="Preto, branco e cores fortes" ligado={ap.tema === 'alto-contraste'} aoMudar={(v) => m({ tema: v ? 'alto-contraste' : 'sistema' })} />
        <Interruptor rotulo="Fonte de leitura fácil" dica="Atkinson Hyperlegible, feita para baixa visão" ligado={ap.fonte === 'atkinson'} aoMudar={(v) => m({ fonte: v ? 'atkinson' : 'schibsted' })} />
      </div>
    </section>
  )
}

// ---------- instalar ----------

let pedidoInstalacao = null
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault()
  pedidoInstalacao = e
})

function ProgramaWindows() {
  const [auto, setAuto] = useState(null)
  const [versao, setVersao] = useState('')
  const [erro, setErro] = useState('')
  useEffect(() => {
    if (!noDesktop) return
    inicioAutomatico.ligado().then(setAuto).catch(() => setAuto(false))
    versaoDesktop().then(setVersao)
  }, [])
  if (noDesktop) {
    return (
      <section>
        <h2>Programa para Windows</h2>
        <p className="dica">Você está usando o Prosa para Windows{versao ? ` (versão ${versao})` : ''}. Fechar a janela deixa o Prosa no ícone ao lado do relógio; para sair de vez, clique com o botão direito nele.</p>
        <Interruptor
          rotulo="Abrir junto com o Windows"
          dica="O Prosa começa escondido no ícone ao lado do relógio"
          ligado={!!auto}
          desativado={auto === null}
          aoMudar={async (v) => {
            setErro('')
            try {
              await inicioAutomatico.mudar(v)
              setAuto(v)
            } catch (e) {
              setErro(String(e))
            }
          }}
        />
        {erro && <p className="erro">{erro}</p>}
        <p className="dica">“Pressionar para falar” funciona mesmo com outra janela na frente, inclusive jogos em tela cheia.</p>
      </section>
    )
  }
  return (
    <section>
      <h2>Programa para Windows</h2>
      <div className="instalar">
        <img src="/icone-192.png" alt="" width="96" height="96" />
        <div>
          <p>
            Com o programa, o <strong>pressionar para falar funciona com o Prosa em segundo plano</strong> (até em jogo em tela cheia), o Prosa fica no
            ícone ao lado do relógio e pode abrir junto com o Windows.
          </p>
          <a className="botao primario grande" href={LINK_DOWNLOAD} target="_blank" rel="noopener noreferrer">
            Baixar para Windows
          </a>
          <p className="dica">Na página que abrir, baixe o arquivo que termina em <code>-setup.exe</code>. Se o Windows avisar que o programa não é conhecido, clique em “Mais informações” → “Executar assim mesmo”.</p>
        </div>
      </div>
    </section>
  )
}

function Instalar() {
  const [pode, setPode] = useState(!!pedidoInstalacao)
  const instalado = window.matchMedia('(display-mode: standalone)').matches
  async function instalar() {
    if (!pedidoInstalacao) return
    pedidoInstalacao.prompt()
    await pedidoInstalacao.userChoice.catch(() => {})
    pedidoInstalacao = null
    setPode(false)
  }
  return (
    <section>
      <h2>Instalar o Prosa</h2>
      <div className="instalar">
        <img src="/icone-192.png" alt="" width="96" height="96" />
        <div>
          {instalado ? (
            <p>Você já está usando o Prosa instalado.</p>
          ) : (
            <>
              <p>Instale o Prosa para abrir numa janela própria, com ícone na área de trabalho e na barra de tarefas, sem as abas do navegador.</p>
              {pode ? (
                <button className="botao primario grande" onClick={instalar}>
                  Instalar o Prosa
                </button>
              ) : (
                <ul className="dica">
                  <li><strong>Chrome ou Edge no computador:</strong> clique no ícone de instalar no fim da barra de endereço (um monitor com seta).</li>
                  <li><strong>Android:</strong> menu ⋮ do Chrome → “Instalar app” ou “Adicionar à tela inicial”.</li>
                  <li><strong>iPhone:</strong> no Safari, botão de compartilhar → “Adicionar à Tela de Início”.</li>
                </ul>
              )}
            </>
          )}
        </div>
      </div>
    </section>
  )
}
