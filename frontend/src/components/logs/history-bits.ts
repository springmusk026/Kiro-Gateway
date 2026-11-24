/** Small helpers shared by the database-backed logs views and the overview. */
import type { MessageKey } from '@/locales/en'

export const fmtMs = (ms: number) =>
  ms >= 10_000 ? `${(ms / 1000).toFixed(1)}s` : ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`

export const fmtBytes = (n: number) => {
  if (!n) return '0 B'
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

/** RFC3339 → unix seconds (for the shared `timeAgo` helper). */
export const unix = (iso: string) => Date.parse(iso) / 1000

export function exactTime(iso: string, lang: string) {
  return new Date(iso).toLocaleString(lang, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export type RangeId = '1h' | '24h' | '7d' | '30d' | 'all'
export const RANGES: { id: RangeId; hours: number; label: MessageKey }[] = [
  { id: '1h', hours: 1, label: 'logs.range.1h' },
  { id: '24h', hours: 24, label: 'logs.range.24h' },
  { id: '7d', hours: 168, label: 'logs.range.7d' },
  { id: '30d', hours: 720, label: 'logs.range.30d' },
  { id: 'all', hours: 0, label: 'logs.range.all' },
]

export const HISTORY_ENDPOINTS = ['claude', 'openai', 'responses', 'count_tokens'] as const

/** "claude-cli/1.0.80 (external, cli)" → "claude-cli 1.0.80"; browsers → "Chrome 131". */
export function shortUA(ua: string | undefined): string {
  if (!ua) return ''
  if (/^Mozilla\//.test(ua)) {
    const m = ua.match(/(Edg|OPR|Firefox|Chrome)\/(\d+)/)
    if (m) return `${m[1] === 'Edg' ? 'Edge' : m[1] === 'OPR' ? 'Opera' : m[1]} ${m[2]}`
    const safari = ua.match(/Version\/(\d+).*Safari\//)
    return safari ? `Safari ${safari[1]}` : 'Browser'
  }
  const first = ua.split(/\s+/)[0]
  const i = first.lastIndexOf('/')
  if (i > 0) {
    const name = first.slice(0, i)
    const ver = first.slice(i + 1)
    return /^v?\d/.test(ver) ? `${name} ${ver.replace(/^v/, '')}` : first
  }
  return first.length > 32 ? first.slice(0, 32) + '…' : first
}

/** Pretty-print JSON when parseable; otherwise return the text unchanged. */
export function prettyBody(text: string): { text: string; json: boolean } {
  const s = text.trim()
  if (s.startsWith('{') || s.startsWith('[')) {
    try {
      return { text: JSON.stringify(JSON.parse(s), null, 2), json: true }
    } catch {
      /* truncated or not JSON */
    }
  }
  return { text, json: false }
}

/** Parse the backend name from GET /storage ("postgres" | "json:<path>"). */
export function parseBackend(backend: string): { kind: 'postgres' | 'json' | 'other'; path?: string } {
  if (backend === 'postgres') return { kind: 'postgres' }
  if (backend.startsWith('json:')) return { kind: 'json', path: backend.slice(5) }
  return { kind: 'other', path: backend }
}
