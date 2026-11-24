/**
 * Admin API client. Every call carries the admin password in the
 * X-Admin-Password header (the only auth the backend accepts).
 */
const PW_KEY = 'kiro_admin_password'
const REMEMBER_KEY = 'kiro_remember'

export class ApiError extends Error {
  status: number
  body: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

export function getPassword(): string {
  try {
    return sessionStorage.getItem(PW_KEY) || localStorage.getItem(PW_KEY) || ''
  } catch {
    return ''
  }
}

export function setPassword(pw: string, remember: boolean) {
  try {
    sessionStorage.setItem(PW_KEY, pw)
    if (remember) {
      localStorage.setItem(PW_KEY, pw)
      localStorage.setItem(REMEMBER_KEY, '1')
    } else {
      localStorage.removeItem(PW_KEY)
      localStorage.removeItem(REMEMBER_KEY)
    }
  } catch {
    /* storage unavailable */
  }
}

export function clearPassword() {
  try {
    sessionStorage.removeItem(PW_KEY)
    localStorage.removeItem(PW_KEY)
    localStorage.removeItem(REMEMBER_KEY)
  } catch {
    /* storage unavailable */
  }
}

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

export async function apiFetch(path: string, init: RequestInit = {}, password = getPassword()): Promise<Response> {
  const headers = new Headers(init.headers)
  headers.set('X-Admin-Password', password)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const res = await fetch('/admin/api' + path, { ...init, headers })
  if (res.status === 401) window.dispatchEvent(new CustomEvent('kiro:unauthorized'))
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
