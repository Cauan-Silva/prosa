import { useEffect } from 'react'
import Entrar from './paginas/Entrar.jsx'
import Aplicativo from './paginas/Aplicativo.jsx'
import { EstadoProvider } from './servicos/estado.jsx'
import { VozProvider } from './servicos/voz.jsx'
import { navegar, useCaminho } from './servicos/rota.jsx'
import { useSessao } from './servicos/sessao.jsx'
import { aplicarTema } from './servicos/tema.js'

export default function App() {
  const { usuario, carregando } = useSessao()
  const caminho = useCaminho()

  useEffect(() => aplicarTema(), [])

  // Guarda o convite aberto antes do login para aceitar depois de entrar.
  const convite = caminho.match(/^\/convite\/([\w-]+)/)?.[1]

  useEffect(() => {
    if (!carregando && !usuario && caminho !== '/entrar' && !convite) navegar('/entrar', { substituir: true })
    if (usuario && caminho === '/entrar') navegar('/', { substituir: true })
  }, [usuario, carregando, caminho, convite])

  if (carregando) return <div className="carregando-tela" aria-label="Carregando" />
  if (!usuario) return <Entrar convite={convite} />
  return (
    <EstadoProvider key={usuario.id}>
      <VozProvider>
        <Aplicativo caminho={caminho} />
      </VozProvider>
    </EstadoProvider>
  )
}
