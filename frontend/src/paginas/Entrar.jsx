import { useEffect, useState } from 'react'
import { Azulejo } from '../componentes/Azulejo.jsx'
import { api } from '../servicos/api.js'
import { useSessao } from '../servicos/sessao.jsx'

// Nomes de servidores de exemplo para a parede de azulejos da tela de entrada.
const parede = [
  'Turma do café', 'Clube do livro', 'Futebol de quinta', 'Estudos de Go', 'Rádio da madrugada',
  'Horta comunitária', 'Pedal de domingo', 'Cozinha da vó', 'Jogatina', 'Projeto final',
  'Vizinhança', 'Trilha sonora', 'Fotografia', 'Xadrez', 'Viagem 2027', 'Bate-papo',
  'Programação', 'Desenho livre', 'Tricô e prosa', 'Cinema em casa', 'Corrida', 'Família',
  'Sebo virtual', 'Escalada',
]

export default function Entrar({ convite }) {
  const { entrar } = useSessao()
  const [modo, setModo] = useState('entrar')
  const [campos, setCampos] = useState({ login: '', senha: '', nomeUsuario: '', nomeExibicao: '', email: '' })
  const [erro, setErro] = useState('')
  const [enviando, setEnviando] = useState(false)
  const [dadosConvite, setDadosConvite] = useState(null)

  useEffect(() => {
    if (!convite) return
    api.verConvite(convite).then(setDadosConvite).catch((e) => setErro(e.message))
    setModo('cadastro')
  }, [convite])

  const mudar = (campo) => (e) => setCampos({ ...campos, [campo]: e.target.value })

  async function enviar(e) {
    e.preventDefault()
    setErro('')
    setEnviando(true)
    try {
      const resposta =
        modo === 'entrar'
          ? await api.entrar(campos.login, campos.senha)
          : await api.cadastrar({
              nomeUsuario: campos.nomeUsuario.trim().toLowerCase(),
              nomeExibicao: campos.nomeExibicao.trim(),
              email: campos.email.trim(),
              senha: campos.senha,
            })
      entrar(resposta)
    } catch (err) {
      setErro(err.message)
      setEnviando(false)
    }
  }

  return (
    <main className="entrada">
      <div className="entrada-parede" aria-hidden="true">
        {Array.from({ length: 96 }, (_, i) => {
          const nome = `${parede[i % parede.length]}${i >= parede.length ? ` ${i}` : ''}`
          return <Azulejo key={i} nome={nome} tamanho={120} />
        })}
      </div>
      <section className="entrada-cartao">
        <h1 className="marca">Prosa</h1>
        {dadosConvite ? (
          <p className="entrada-convite">
            <strong>{dadosConvite.criadorNome}</strong> chamou você para <strong>{dadosConvite.servidorNome}</strong>.
            {modo === 'cadastro' ? ' Crie sua conta para entrar.' : ' Entre na sua conta para aceitar.'}
          </p>
        ) : (
          <p className="entrada-lema">Servidores para conversar com sua turma, por texto e por voz.</p>
        )}

        <div className="abas" role="tablist">
          <button role="tab" aria-selected={modo === 'entrar'} onClick={() => setModo('entrar')}>
            Entrar
          </button>
          <button role="tab" aria-selected={modo === 'cadastro'} onClick={() => setModo('cadastro')}>
            Criar conta
          </button>
        </div>

        <form onSubmit={enviar} className="formulario">
          {modo === 'entrar' ? (
            <label>
              E-mail ou nome de usuário
              <input value={campos.login} onChange={mudar('login')} autoComplete="username" required autoFocus />
            </label>
          ) : (
            <>
              <label>
                Nome de usuário
                <input
                  value={campos.nomeUsuario}
                  onChange={mudar('nomeUsuario')}
                  autoComplete="username"
                  pattern="[a-zA-Z0-9_.]{3,32}"
                  title="De 3 a 32 letras, números, ponto ou sublinhado"
                  required
                  autoFocus
                />
                <small>Único, só letras minúsculas, números, ponto e _. Ex.: cauan.silva</small>
              </label>
              <label>
                Como quer ser chamado
                <input value={campos.nomeExibicao} onChange={mudar('nomeExibicao')} autoComplete="nickname" maxLength={40} required />
              </label>
              <label>
                E-mail
                <input type="email" value={campos.email} onChange={mudar('email')} autoComplete="email" required />
              </label>
            </>
          )}
          <label>
            Senha
            <input
              type="password"
              value={campos.senha}
              onChange={mudar('senha')}
              autoComplete={modo === 'entrar' ? 'current-password' : 'new-password'}
              minLength={modo === 'cadastro' ? 8 : undefined}
              required
            />
            {modo === 'cadastro' && <small>Pelo menos 8 caracteres.</small>}
          </label>
          {erro && (
            <p className="erro" role="alert">
              {erro}
            </p>
          )}
          <button className="botao primario grande" disabled={enviando}>
            {enviando ? 'Um instante…' : modo === 'entrar' ? 'Entrar' : 'Criar conta'}
          </button>
        </form>
      </section>
    </main>
  )
}
