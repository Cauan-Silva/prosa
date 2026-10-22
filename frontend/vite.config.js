import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Em desenvolvimento, o Vite repassa a API e o tempo real para o servidor Go (porta 8080).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api/gateway': { target: 'ws://localhost:8080', ws: true },
      '/api': 'http://localhost:8080',
    },
  },
})
