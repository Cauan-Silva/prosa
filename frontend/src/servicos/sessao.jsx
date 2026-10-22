// Quem está logado, disponível para todas as telas.
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, guardarToken, lerToken } from './api.js'

const Contexto = createContext(null)

export function SessaoProvider({ children }) {
  const [usuario, setUsuario] = useState(null)
  const [carregando, setCarregando] = useState(Boolean(lerToken()))

  const sair = useCallback(() => {
    guardarToken(null)
    setUsuario(null)
  }, [])

  useEffect(() => {
    if (!lerToken()) return
    api
      .eu()
      .then(setUsuario)
      .catch((e) => e.status === 401 && sair())
      .finally(() => setCarregando(false))
  }, [sair])

  useEffect(() => {
    window.addEventListener('sessao-expirada', sair)
    return () => window.removeEventListener('sessao-expirada', sair)
  }, [sair])

  const entrar = useCallback((resposta) => {
    guardarToken(resposta.token)
    setUsuario(resposta.usuario)
  }, [])

  return (
    <Contexto.Provider value={{ usuario, setUsuario, carregando, entrar, sair }}>{children}</Contexto.Provider>
  )
}

export const useSessao = () => useContext(Contexto)
