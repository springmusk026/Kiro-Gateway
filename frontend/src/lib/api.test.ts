import { beforeEach, describe, expect, it, vi } from 'vitest'

type ApiModule = typeof import('./api')

/** A fresh module per test: api.ts keeps the signed-in state at module level. */
async function load(): Promise<ApiModule> {
  vi.resetModules()
  return import('./api')
}

function respond(status: number, body?: unknown) {
  const text = body === undefined ? null : typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(text, { status, headers: { 'Content-Type': 'application/json' } })
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

const lastCall = () => fetchMock.mock.calls.at(-1) as [string, RequestInit]
const header = (init: RequestInit, name: string) => new Headers(init.headers).get(name)

describe('api client', () => {
  it('prefixes /admin/api, sends X-Requested-With and the cookie on every call', async () => {
    const { get } = await load()
    fetchMock.mockResolvedValueOnce(respond(200, { ok: 1 }))
    await expect(get('/status')).resolves.toEqual({ ok: 1 })
    const [url, init] = lastCall()
    expect(url).toBe('/admin/api/status')
    expect(header(init, 'X-Requested-With')).toBe('kiro-console')
    expect(init.credentials).toBe('same-origin')
    expect(header(init, 'Content-Type')).toBeNull()
  })

  it('sends JSON bodies with the right method and content type', async () => {
    const { post, put, del } = await load()
    fetchMock.mockImplementation(async () => respond(200, { success: true }))

    await post('/api-keys', { name: 'ci' })
    let [, init] = lastCall()
    expect(init.method).toBe('POST')
    expect(init.body).toBe('{"name":"ci"}')
    expect(header(init, 'Content-Type')).toBe('application/json')
    expect(header(init, 'X-Requested-With')).toBe('kiro-console')

    await post('/stats/reset')
    ;[, init] = lastCall()
    expect(init.body).toBe('{}')

    await put('/api-keys/1', { enabled: false })
    ;[, init] = lastCall()
    expect(init.method).toBe('PUT')

    await del('/api-keys/1')
    ;[, init] = lastCall()
    expect(init.method).toBe('DELETE')
    expect(init.body).toBeUndefined()
    expect(header(init, 'X-Requested-With')).toBe('kiro-console')
  })

  it('keeps an explicit content type', async () => {
    const { api } = await load()
    fetchMock.mockResolvedValueOnce(respond(200, {}))
    await api('/import', { method: 'POST', body: 'a,b', headers: { 'Content-Type': 'text/csv' } })
    expect(header(lastCall()[1], 'Content-Type')).toBe('text/csv')
  })

  it.each([
    ['a string error', { error: 'bad key' }, 'bad key'],
    ['an error object', { error: { type: 'invalid_request_error', message: 'too long' } }, 'too long'],
    ['a message field', { message: 'nope' }, 'nope'],
    ['no message', { foo: 1 }, 'HTTP 400'],
    ['a plain-text body', 'gateway exploded', 'HTTP 400'],
  ])('throws ApiError with the server message for %s', async (_name, body, message) => {
    const { get, ApiError } = await load()
    fetchMock.mockResolvedValueOnce(respond(400, body))
    const err = await get('/x').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as InstanceType<typeof ApiError>).status).toBe(400)
    expect((err as Error).message).toBe(message)
    expect((err as InstanceType<typeof ApiError>).body).toEqual(body)
  })

  it('treats {success:false} on a 200 as an error', async () => {
    const { post, ApiError } = await load()
    fetchMock.mockResolvedValueOnce(respond(200, { success: false, error: 'duplicate' }))
    const err = await post('/accounts', {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as Error).message).toBe('duplicate')
    fetchMock.mockResolvedValueOnce(respond(200, { success: false }))
    await expect(post('/accounts', {})).rejects.toThrow('Request failed')
  })

  it('returns undefined for an empty body and text for a non-JSON body', async () => {
    const { get } = await load()
    fetchMock.mockResolvedValueOnce(respond(204))
    await expect(get('/x')).resolves.toBeUndefined()
    fetchMock.mockResolvedValueOnce(respond(200, 'plain'))
    await expect(get('/x')).resolves.toBe('plain')
  })

  it('dispatches kiro:unauthorized on a 401 and marks the console signed out', async () => {
    const { get, ensureSession } = await load()
    const onUnauthorized = vi.fn()
    window.addEventListener('kiro:unauthorized', onUnauthorized)
    try {
      fetchMock.mockResolvedValueOnce(respond(401, { error: 'Unauthorized' }))
      await expect(get('/status')).rejects.toMatchObject({ status: 401, message: 'Unauthorized' })
      expect(onUnauthorized).toHaveBeenCalledTimes(1)

      // The next session check goes to the server instead of trusting a cached state.
      fetchMock.mockResolvedValueOnce(respond(401))
      await expect(ensureSession()).resolves.toBe(false)
      expect(lastCall()[0]).toBe('/admin/api/session')
    } finally {
      window.removeEventListener('kiro:unauthorized', onUnauthorized)
    }
  })

  it('does not dispatch kiro:unauthorized for a 401 from /session (wrong password)', async () => {
    const { createSession } = await load()
    const onUnauthorized = vi.fn()
    window.addEventListener('kiro:unauthorized', onUnauthorized)
    try {
      fetchMock.mockResolvedValueOnce(respond(401, { error: 'Invalid password' }))
      await expect(createSession('wrong', false)).resolves.toBe(401)
      const [url, init] = lastCall()
      expect(url).toBe('/admin/api/session')
      expect(init.method).toBe('POST')
      expect(JSON.parse(init.body as string)).toEqual({ password: 'wrong', remember: false })
      expect(onUnauthorized).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('kiro:unauthorized', onUnauthorized)
    }
  })

  it('does not sign out on a 401 while re-authenticating after a password change', async () => {
    const { withReauth, post } = await load()
    const onUnauthorized = vi.fn()
    window.addEventListener('kiro:unauthorized', onUnauthorized)
    try {
      fetchMock
        .mockResolvedValueOnce(respond(200, { authenticated: true, remember: true })) // GET /session
        .mockResolvedValueOnce(respond(401, { error: 'session ended' })) // the change itself
        .mockResolvedValueOnce(respond(200, { authenticated: true })) // POST /session
      await expect(withReauth('new-pw', () => post('/settings', { password: 'new-pw' }))).rejects.toThrow('session ended')
      expect(onUnauthorized).not.toHaveBeenCalled()

      fetchMock.mockReset()
      fetchMock
        .mockResolvedValueOnce(respond(200, { authenticated: true, remember: true }))
        .mockResolvedValueOnce(respond(200, { success: true }))
        .mockResolvedValueOnce(respond(200, { authenticated: true }))
      await withReauth('new-pw', () => post('/settings', { password: 'new-pw' }))
      const [url, init] = lastCall()
      expect(url).toBe('/admin/api/session')
      expect(JSON.parse(init.body as string)).toEqual({ password: 'new-pw', remember: true })
    } finally {
      window.removeEventListener('kiro:unauthorized', onUnauthorized)
    }
  })

  it('ensureSession caches a successful sign-in', async () => {
    const { createSession, ensureSession } = await load()
    fetchMock.mockResolvedValueOnce(respond(200, { authenticated: true }))
    expect(await createSession('pw', true)).toBe(200)
    fetchMock.mockClear()
    await expect(ensureSession()).resolves.toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('ensureSession exchanges a legacy stored password once and deletes it', async () => {
    const { ensureSession } = await load()
    localStorage.setItem('kiro_admin_password', 'legacy-pw')
    localStorage.setItem('kiro_remember', '1')
    fetchMock.mockResolvedValueOnce(respond(200, { authenticated: true }))
    await expect(ensureSession()).resolves.toBe(true)
    const [url, init] = lastCall()
    expect(url).toBe('/admin/api/session')
    expect(JSON.parse(init.body as string)).toEqual({ password: 'legacy-pw', remember: true })
    expect(localStorage.getItem('kiro_admin_password')).toBeNull()
    expect(localStorage.getItem('kiro_remember')).toBeNull()
  })

  it('ensureSession is false when the network fails', async () => {
    const { ensureSession } = await load()
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await expect(ensureSession()).resolves.toBe(false)
  })

  it('deleteSession ignores network errors', async () => {
    const { deleteSession } = await load()
    fetchMock.mockRejectedValueOnce(new TypeError('offline'))
    await expect(deleteSession()).resolves.toBeUndefined()
    expect(lastCall()[1].method).toBe('DELETE')
  })
})
