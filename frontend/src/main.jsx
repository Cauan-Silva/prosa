import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.jsx'
import { SessaoProvider } from './servicos/sessao.jsx'
import './estilos.css'

createRoot(document.getElementById('raiz')).render(
  <StrictMode>
    <SessaoProvider>
      <App />
    </SessaoProvider>
  </StrictMode>,
)
