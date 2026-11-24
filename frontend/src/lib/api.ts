/**
 * Admin API client. The console authenticates with an HttpOnly session
 * cookie (POST /admin/api/session); the password is never stored in the
 * browser. Every call carries X-Requested-With, which the server requires on
 * state-changing calls authenticated by cookie (CSRF protection).
 */

// Older builds kept the password in web storage; it is exchanged for a
// session once and then deleted.
const LEGACY_PW_KEY = 'kiro_admin_password'
const LEGACY_REMEMBER_KEY = 'kiro_remember'

export class ApiError extends Error {
  status: number
  body: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

export interface SessionInfo {
  authenticated: boolean
  via?: 'cookie' | 'header'
  remember?: boolean
  createdAt?: number
  expiresAt?: number
  idleExpiresAt?: number
}

/** Known session state: null = not checked yet. */
let signedIn: boolean | null = null
/** While > 0, 401s do not sign the console out (password change in progress). */
let reauthenticating = 0

function errorMessage(body: unknown, fallback: string): string {
  if (body && typeof body === 'object') {
    const b = body as Record<string, unknown>
    const e = b.error
    if (typeof e === 'string') return e
    if (e && typeof e === 'object' && typeof (e as Record<string, unknown>).message === 'string') {
      return (e as Record<string, string>).message
    }
    if (typeof b.message === 'string') return b.message
  }
  return fallback
}

function adminHeaders(init: RequestInit): Headers {
  const headers = new Headers(init.headers)
  headers.set('X-Requested-With', 'kiro-console')
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  return headers
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const res = await fetch('/admin/api' + path, { ...init, headers: adminHeaders(init), credentials: 'same-origin' })
  if (res.status === 401 && path !== '/session' && reauthenticating === 0) {
    signedIn = false
    window.dispatchEvent(new CustomEvent('kiro:unauthorized'))
  }
  return res
}

export async function api<T = unknown>(path: string, init: RequestInit & { json?: unknown } = {}): Promise<T> {
  const { json, ...rest } = init
  const res = await apiFetch(path, json !== undefined ? { ...rest, body: JSON.stringify(json) } : rest)
  const text = await res.text()
  let body: unknown = undefined
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = text
    }
  }
  if (!res.ok) throw new ApiError(res.status, errorMessage(body, `HTTP ${res.status}`), body)
  if (body && typeof body === 'object' && (body as Record<string, unknown>).success === false) {
    throw new ApiError(res.status, errorMessage(body, 'Request failed'), body)
  }
  return body as T
}

export const get = <T,>(path: string) => api<T>(path)
export const post = <T,>(path: string, json?: unknown) => api<T>(path, { method: 'POST', json: json ?? {} })
export const put = <T,>(path: string, json?: unknown) => api<T>(path, { method: 'PUT', json: json ?? {} })
export const del = <T,>(path: string) => api<T>(path, { method: 'DELETE' })

// ---------- session ----------

/** POST /session. Returns the HTTP status (200 ok, 401 wrong, 429 locked). */
export async function createSession(password: string, remember: boolean): Promise<number> {
  const res = await apiFetch('/session', { method: 'POST', body: JSON.stringify({ password, remember }) })
  if (res.ok) signedIn = true
  return res.status
}

/** DELETE /session (sign out). Errors are ignored: the cookie is cleared server-side when reachable. */
export async function deleteSession(): Promise<void> {
  signedIn = false
  try {
    await apiFetch('/session', { method: 'DELETE' })
  } catch {
    /* offline: the session expires on its own */
  }
}

export async function getSession(): Promise<SessionInfo | null> {
  const res = await apiFetch('/session')
  if (!res.ok) return null
  return (await res.json()) as SessionInfo
}

/** Marks the console signed out locally (after a 401). */
export function markSignedOut() {
  signedIn = false
}

function takeLegacyPassword(): { pw: string; remember: boolean } | null {
  try {
    const pw = sessionStorage.getItem(LEGACY_PW_KEY) || localStorage.getItem(LEGACY_PW_KEY) || ''
    const remember = localStorage.getItem(LEGACY_REMEMBER_KEY) === '1'
    sessionStorage.removeItem(LEGACY_PW_KEY)
    localStorage.removeItem(LEGACY_PW_KEY)
    localStorage.removeItem(LEGACY_REMEMBER_KEY)
    return pw ? { pw, remember } : null
  } catch {
    return null
  }
}

/**
 * Resolves whether the console has a session, exchanging a password left in
 * web storage by an older build for a session once (and deleting it).
 */
export async function ensureSession(): Promise<boolean> {
  if (signedIn) return true
  const legacy = takeLegacyPassword()
  if (legacy) {
    try {
      if ((await createSession(legacy.pw, legacy.remember)) === 200) return true
    } catch {
      /* fall through to the session check */
    }
  }
  try {
    signedIn = (await getSession()) !== null
  } catch {
    signedIn = false
  }
  return signedIn
}

/**
 * Runs fn (which changes the admin password, ending every session) and signs
 * in again with the new password without bouncing to the login page.
 */
export async function withReauth<T>(newPassword: string, fn: () => Promise<T>): Promise<T> {
  reauthenticating++
  try {
    const remember = (await getSession().catch(() => null))?.remember ?? false
    const out = await fn()
    await createSession(newPassword, remember)
    return out
  } finally {
    reauthenticating--
  }
}
