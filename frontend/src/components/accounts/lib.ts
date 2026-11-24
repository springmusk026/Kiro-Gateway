import { useCallback, useSyncExternalStore } from 'react'
import { ApiError } from '@/lib/api'
import type { ExportAccount } from '@/lib/queries.accounts'
import type { Account, AccountFull, CredentialImport } from '@/lib/types'
import type { TFn } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'

/* ---------- health ---------- */

export type Health = 'banned' | 'suspended' | 'sysDisabled' | 'disabled' | 'noToken' | 'expired' | 'quota' | 'healthy'

export const isBanned = (a: Account) => !!a.banStatus && a.banStatus !== 'ACTIVE'
export const isExpired = (a: Account) => !!a.expiresAt && a.expiresAt < Date.now() / 1000
export const isOverQuota = (a: Account) => a.usageLimit > 0 && a.usagePercent >= 1

/** The single most important state of an account, used for the dot and sorting. */
export function health(a: Account): Health {
  if (isBanned(a)) {
    if (a.banStatus === 'SUSPENDED') return 'suspended'
    if (a.banStatus === 'BANNED') return 'banned'
    return 'sysDisabled'
  }
  if (!a.enabled) return 'disabled'
  if (!a.hasToken) return 'noToken'
  if (isOverQuota(a)) return 'quota'
  if (isExpired(a)) return 'expired'
  return 'healthy'
}

/** Can the router send traffic to it right now? (expired access tokens refresh on demand) */
export const isAvailable = (a: Account) => {
  const h = health(a)
  return h === 'healthy' || h === 'expired'
}
export const needsAttention = (a: Account) => {
  const h = health(a)
  return h === 'banned' || h === 'suspended' || h === 'sysDisabled' || h === 'noToken' || h === 'quota'
}

export type StatusFilter = 'all' | 'enabled' | 'disabled' | 'banned' | 'available' | 'attention'
export function matchesStatus(a: Account, f: StatusFilter) {
  switch (f) {
    case 'all':
      return true
    case 'enabled':
      return a.enabled
    case 'disabled':
      return !a.enabled && !isBanned(a)
    case 'banned':
      return isBanned(a)
    case 'available':
      return isAvailable(a)
    case 'attention':
      return needsAttention(a)
  }
}

export type SortKey = 'default' | 'name' | 'usage' | 'requests' | 'lastUsed' | 'expiry' | 'weight' | 'health'
const healthRank: Record<Health, number> = {
  banned: 0,
  suspended: 0,
  sysDisabled: 1,
  noToken: 2,
  quota: 3,
  expired: 4,
  disabled: 5,
  healthy: 6,
}
export function sortAccounts(list: Account[], key: SortKey): Account[] {
  if (key === 'default') return list
  const s = list.slice()
  const by = <T,>(f: (a: Account) => T, dir: 1 | -1) =>
    s.sort((a, b) => {
      const x = f(a) as unknown as number | string
      const y = f(b) as unknown as number | string
      return x < y ? -dir : x > y ? dir : 0
    })
  switch (key) {
    case 'name':
      return by((a) => (a.nickname || a.email || a.id).toLowerCase(), 1)
    case 'usage':
      return by((a) => (a.usageLimit > 0 ? a.usagePercent : -1), -1)
    case 'requests':
      return by((a) => a.requestCount, -1)
    case 'lastUsed':
      return by((a) => a.lastUsed, -1)
    case 'expiry':
      return by((a) => a.expiresAt || Number.MAX_SAFE_INTEGER, 1)
    case 'weight':
      return by((a) => a.weight, -1)
    case 'health':
      return by((a) => healthRank[health(a)], 1)
  }
}

export function matchesSearch(a: Account, q: string) {
  if (!q) return true
  const s = q.toLowerCase()
  return [a.email, a.nickname, a.id, a.provider, a.authMethod, a.region, a.subscriptionTitle, a.subscriptionType].some(
    (v) => v && v.toLowerCase().includes(s),
  )
}

/* ---------- labels ---------- */

export function authLabel(a: Pick<Account, 'provider' | 'authMethod'>, t: TFn): string {
  const k = authLabelKey(a)
  return k.startsWith('accounts.auth.') ? t(k as MessageKey) : k
}

function authLabelKey(a: Pick<Account, 'provider' | 'authMethod'>): string {
  const v = (a.provider || a.authMethod || '').toLowerCase()
  switch (v) {
    case 'external_idp':
    case 'azuread':
      return 'accounts.auth.microsoft'
    case 'idc':
    case 'enterprise':
      return 'accounts.auth.enterprise'
    case 'social':
      return 'accounts.auth.social'
    case 'api_key':
    case 'apikey':
      return 'accounts.auth.apiKey'
    case 'builderid':
      return 'accounts.auth.builderId'
    case 'github':
      return 'accounts.auth.github'
    case 'google':
      return 'accounts.auth.google'
    default:
      return a.provider || a.authMethod || '–'
  }
}

export type PlanTier = 'power' | 'proPlus' | 'pro' | 'free' | 'other'
export function planTier(a: Pick<Account, 'subscriptionType'>): PlanTier {
  const s = (a.subscriptionType || '').toUpperCase()
  if (!s) return 'other'
  if (s.includes('POWER')) return 'power'
  if (s.includes('PRO_PLUS') || s.includes('PROPLUS')) return 'proPlus'
  if (s.includes('PRO')) return 'pro'
  if (s.includes('FREE')) return 'free'
  return 'other'
}

export const isApiKeyAccount = (a: Pick<Account, 'authMethod'>) => {
  const m = (a.authMethod || '').toLowerCase()
  return m === 'api_key' || m === 'apikey'
}

/* ---------- privacy mode (localStorage 'privacyMode', default on) ---------- */

const PRIVACY_KEY = 'privacyMode'
const PRIVACY_EVENT = 'kiro:privacy'
function readPrivacy(): boolean {
  try {
    return localStorage.getItem(PRIVACY_KEY) !== 'false'
  } catch {
    return true
  }
}
function subscribePrivacy(cb: () => void) {
  window.addEventListener(PRIVACY_EVENT, cb)
  window.addEventListener('storage', cb)
  return () => {
    window.removeEventListener(PRIVACY_EVENT, cb)
    window.removeEventListener('storage', cb)
  }
}
export function usePrivacyMode() {
  const on = useSyncExternalStore(subscribePrivacy, readPrivacy, () => true)
  const set = useCallback((v: boolean) => {
    try {
      localStorage.setItem(PRIVACY_KEY, String(v))
    } catch {
      /* storage unavailable */
    }
    window.dispatchEvent(new Event(PRIVACY_EVENT))
  }, [])
  return [on, set] as const
}

/* ---------- credentials helpers ---------- */

const trimObj = <T extends Record<string, unknown>>(o: T) =>
  Object.fromEntries(Object.entries(o).filter(([, v]) => v !== '' && v != null)) as Partial<T>

/** Re-importable credentials JSON for one account (same shape the old UI copied). */
export function credentialsFromFull(f: AccountFull) {
  return {
    clientId: f.clientId || '',
    clientSecret: f.clientSecret || '',
    accessToken: f.accessToken || '',
    refreshToken: f.refreshToken || '',
    ...trimObj({
      authMethod: f.authMethod,
      provider: f.provider,
      tokenEndpoint: f.tokenEndpoint,
      issuerUrl: f.issuerUrl,
      scopes: f.scopes,
      userId: f.userId,
      profileArn: f.profileArn,
      region: f.region,
    }),
  }
}

/** Export row → import payload (the "Copy JSON" output of the export dialog). */
export function credentialsFromExport(a: ExportAccount) {
  const c = a.credentials || ({} as ExportAccount['credentials'])
  return {
    clientId: c.clientId || '',
    clientSecret: c.clientSecret || '',
    accessToken: c.accessToken || '',
    refreshToken: c.refreshToken || '',
    ...trimObj({
      authMethod: c.authMethod,
      provider: c.provider || a.idp,
      tokenEndpoint: c.tokenEndpoint,
      issuerUrl: c.issuerUrl,
      scopes: c.scopes,
      userId: a.userId,
      profileArn: a.profileArn,
      region: c.region,
    }),
  }
}

type Rec = Record<string, unknown>
const str = (v: unknown) => (typeof v === 'string' ? v.trim() : v == null ? '' : String(v).trim())

/** Accepts a plain record or a Kiro Account Manager export row. */
export function normalizeCredentialRecord(record: unknown): CredentialImport {
  const src = (record && typeof record === 'object' ? record : {}) as Rec
  const creds = (src.credentials && typeof src.credentials === 'object' ? src.credentials : {}) as Rec
  const v = (k: string) => str(Object.prototype.hasOwnProperty.call(creds, k) ? creds[k] : src[k])
  return {
    id: v('id'),
    email: v('email'),
    userId: v('userId'),
    nickname: v('nickname'),
    profileArn: v('profileArn'),
    accessToken: v('accessToken'),
    refreshToken: v('refreshToken'),
    kiroApiKey: v('kiroApiKey'),
    clientId: v('clientId'),
    clientSecret: v('clientSecret'),
    authMethod: v('authMethod'),
    provider: v('provider') || str(src.idp),
    region: v('region'),
    tokenEndpoint: v('tokenEndpoint'),
    issuerUrl: v('issuerUrl'),
    scopes: v('scopes'),
  }
}

/** `email----password----refreshToken----clientId----clientSecret` (or tab / space separated). */
export function parseLineCredentials(text: string) {
  const items: CredentialImport[] = []
  let skipped = 0
  for (const line of text.split(/\r?\n/)) {
    const t = line.trim()
    if (!t) continue
    const parts = (t.includes('----') ? t.split('----') : t.includes('\t') ? t.split(/\t+/) : t.split(/\s+/)).map((s) =>
      s.trim(),
    )
    if (parts.length < 5 || !parts[2]) {
      skipped++
      continue
    }
    items.push({ email: parts[0], refreshToken: parts[2], clientId: parts[3], clientSecret: parts[4] })
  }
  return { items, skipped }
}

export type ParsedCredentials =
  | { ok: true; format: 'json' | 'lines'; items: CredentialImport[]; skipped: number }
  | { ok: false; reason: 'empty' | 'invalid' | 'allSkipped'; skipped: number }

export function parseCredentialsInput(raw: string): ParsedCredentials {
  const text = raw.trim()
  if (!text) return { ok: false, reason: 'empty', skipped: 0 }
  try {
    const json = JSON.parse(text) as unknown
    const src =
      json && typeof json === 'object' && Array.isArray((json as Rec).accounts)
        ? ((json as Rec).accounts as unknown[])
        : Array.isArray(json)
          ? json
          : [json]
    return { ok: true, format: 'json', items: src.map(normalizeCredentialRecord), skipped: 0 }
  } catch {
    const { items, skipped } = parseLineCredentials(text)
    if (!items.length) return { ok: false, reason: skipped ? 'allSkipped' : 'invalid', skipped }
    return { ok: true, format: 'lines', items, skipped }
  }
}

const EXTERNAL_ALIASES = [
  'external_idp',
  'external-idp',
  'external',
  'microsoft',
  'm365',
  'office365',
  'azure',
  'azuread',
  'azure-ad',
  'azure_ad',
  'entra',
  'entra-id',
]

/** Builds the POST /auth/credentials body for one parsed record, or null when it lacks a token. */
export function credentialPayload(item: CredentialImport): CredentialImport | null {
  const methodKey = (item.authMethod || '').trim().toLowerCase()
  const rawProvider = (item.provider || '').trim()
  const providerKey = rawProvider.toLowerCase()
  const kiroApiKey = (item.kiroApiKey || '').trim()
  const isApiKey =
    !!kiroApiKey ||
    methodKey === 'api_key' ||
    methodKey === 'apikey' ||
    (!item.refreshToken && (item.accessToken || '').trim().startsWith('ksk_'))
  if (isApiKey) {
    const key = kiroApiKey || (item.accessToken || '').trim()
    if (!key) return null
    return {
      id: item.id || '',
      email: item.email || '',
      userId: item.userId || '',
      nickname: item.nickname || '',
      kiroApiKey: key,
      authMethod: 'api_key',
      provider: rawProvider || 'APIKey',
      region: item.region || 'us-east-1',
    }
  }
  if (!item.refreshToken) return null
  const ext =
    EXTERNAL_ALIASES.includes(methodKey) ||
    EXTERNAL_ALIASES.includes(providerKey) ||
    !!(item.tokenEndpoint || item.issuerUrl)
  let authMethod: string
  if (ext) authMethod = 'external_idp'
  else if (item.clientId && item.clientSecret) authMethod = 'idc'
  else if (methodKey === 'idc') authMethod = 'idc'
  else if (methodKey) authMethod = 'social'
  else authMethod = ''
  let provider = ext ? 'AzureAD' : rawProvider
  if (!provider && authMethod === 'social') provider = 'Google'
  if (!provider && authMethod === 'idc') provider = 'BuilderId'
  return {
    id: item.id || '',
    email: item.email || '',
    userId: item.userId || '',
    nickname: item.nickname || '',
    profileArn: item.profileArn || '',
    refreshToken: item.refreshToken,
    accessToken: item.accessToken || '',
    clientId: item.clientId || '',
    clientSecret: item.clientSecret || '',
    authMethod,
    provider,
    region: item.region || (ext ? '' : 'us-east-1'),
    tokenEndpoint: item.tokenEndpoint || '',
    issuerUrl: item.issuerUrl || '',
    scopes: item.scopes || '',
  }
}

/* ---------- errors ---------- */

export interface ErrorInfo {
  message: string
  status?: number
  rotatedRefreshToken?: string
  hint?: string
}
export function errorInfo(e: unknown): ErrorInfo {
  if (e instanceof ApiError) {
    const b = (e.body && typeof e.body === 'object' ? e.body : {}) as Rec
    return {
      message: e.message,
      status: e.status,
      rotatedRefreshToken: typeof b.rotatedRefreshToken === 'string' ? b.rotatedRefreshToken : undefined,
      hint: typeof b.hint === 'string' ? b.hint : undefined,
    }
  }
  return { message: e instanceof Error ? e.message : String(e) }
}
export const errMsg = (e: unknown) => errorInfo(e).message

/** A fetch that never reached the server (offline, DNS, reset) rather than an HTTP error. */
export const isNetworkError = (e: unknown) => !(e instanceof ApiError) && !(e instanceof DOMException)

export const MACHINE_ID_RE = /^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{32})$/i
export const PROXY_RE = /^(socks5|socks5h|http|https):\/\/\S+$/i

export const REGIONS = [
  'us-east-1',
  'us-east-2',
  'us-west-2',
  'eu-central-1',
  'eu-west-1',
  'eu-west-2',
  'ap-northeast-1',
  'ap-southeast-1',
  'ap-southeast-2',
  'ap-south-1',
]

export function displayName(a: Pick<Account, 'nickname' | 'email' | 'id'>, privacy: boolean, mask: (s: string) => string) {
  if (a.nickname) return a.nickname
  if (a.email) return privacy && a.email.includes('@') ? mask(a.email) : a.email
  return a.id.slice(0, 12) + '…'
}

export function downloadJson(data: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
