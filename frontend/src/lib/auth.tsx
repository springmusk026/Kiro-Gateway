import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { createContext, useCallback, useContext, useEffect, type ReactNode } from 'react'
import { createSession, deleteSession, markSignedOut } from './api'

type LoginResult = 'ok' | 'wrong' | 'locked' | 'unreachable'
const Ctx = createContext<{ login: (pw: string, remember: boolean) => Promise<LoginResult>; logout: () => void }>({
  login: async () => 'unreachable',
  logout: () => {},
})

/** Exchanges the password for a session cookie. */
export async function signIn(pw: string, remember: boolean): Promise<LoginResult> {
  try {
    const status = await createSession(pw, remember)
    if (status === 200) return 'ok'
    if (status === 429) return 'locked'
    if (status === 401) return 'wrong'
    return 'unreachable'
  } catch {
    return 'unreachable'
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  // Session already gone (401): clear local state only.
  const signedOut = useCallback(() => {
    markSignedOut()
    qc.clear()
    navigate({ to: '/login' })
  }, [navigate, qc])
  // Explicit sign-out: end the session on the server too.
  const logout = useCallback(() => {
    void deleteSession().finally(signedOut)
  }, [signedOut])
  const login = useCallback((pw: string, remember: boolean) => signIn(pw, remember), [])
  useEffect(() => {
    window.addEventListener('kiro:unauthorized', signedOut)
    return () => window.removeEventListener('kiro:unauthorized', signedOut)
  }, [signedOut])
  return <Ctx.Provider value={{ login, logout }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)
