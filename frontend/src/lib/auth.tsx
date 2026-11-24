import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { createContext, useCallback, useContext, useEffect, type ReactNode } from 'react'
import { ApiError, apiFetch, clearPassword, setPassword } from './api'

type LoginResult = 'ok' | 'wrong' | 'locked' | 'unreachable'
const Ctx = createContext<{ login: (pw: string, remember: boolean) => Promise<LoginResult>; logout: () => void }>({
  login: async () => 'unreachable',
  logout: () => {},
})

export async function checkPassword(pw: string): Promise<LoginResult> {
  try {
    const res = await apiFetch('/status', {}, pw)
    if (res.ok) return 'ok'
    if (res.status === 429) return 'locked'
    if (res.status === 401) return 'wrong'
    return 'unreachable'
  } catch (e) {
    return e instanceof ApiError ? 'wrong' : 'unreachable'
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const logout = useCallback(() => {
    clearPassword()
    qc.clear()
    navigate({ to: '/login' })
  }, [navigate, qc])
  const login = useCallback(async (pw: string, remember: boolean) => {
    const r = await checkPassword(pw)
    if (r === 'ok') setPassword(pw, remember)
    return r
  }, [])
  useEffect(() => {
    const onUnauthorized = () => logout()
    window.addEventListener('kiro:unauthorized', onUnauthorized)
    return () => window.removeEventListener('kiro:unauthorized', onUnauthorized)
  }, [logout])
  return <Ctx.Provider value={{ login, logout }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)
