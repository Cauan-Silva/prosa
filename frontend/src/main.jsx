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

// Permite instalar o Prosa como aplicativo (Chrome/Edge: ícone de instalar na barra de endereço).
if ('serviceWorker' in navigator && location.protocol === 'https:') {
  navigator.serviceWorker.register('/sw.js').catch(() => {})
}
