export function compact(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '–'
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, '') + 'B'
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K'
  return String(n)
}

export function fixed(n: number | null | undefined, digits = 1): string {
  if (n == null || Number.isNaN(n)) return '–'
  return n.toFixed(digits)
}

export function percent(part: number, total: number): string {
  if (!total) return '–'
  return ((100 * part) / total).toFixed(1) + '%'
}

/**
 * US dollars. Below $1 keeps up to four significant digits ($0.0031, $0.1235),
 * from $1 two decimals ($12.50, $1,234.56); amounts of a million or more (or of
 * ten thousand or more with `compact`) are abbreviated ($12.3K, $4.5M).
 */
export function usd(n: number | null | undefined, opts: { compact?: boolean } = {}): string {
  if (n == null || !Number.isFinite(n)) return '–'
  const sign = n < 0 ? '-' : ''
  const a = Math.abs(n)
  if (a === 0) return '$0.00'
  if (a < 0.0001) return sign + '<$0.0001'
  if (a >= (opts.compact ? 1e4 : 1e6)) return sign + '$' + compact(a)
  if (a >= 1) return sign + '$' + a.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  // Below $1: four significant digits, at least two decimals, trailing zeros trimmed.
  const digits = Math.min(6, Math.max(2, Math.ceil(-Math.log10(a)) + 3))
  let s = a.toFixed(digits)
  s = s.replace(/0+$/, '')
  const dot = s.indexOf('.')
  if (s.length - dot - 1 < 2) s = s.padEnd(dot + 3, '0')
  if (s === '1.00') return sign + '$1.00'
  return sign + '$' + s
}

/** A list price per million tokens: whole dollars stay whole ($3), cents keep two to three decimals ($0.30, $0.075). */
export function usdRate(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '–'
  if (Number.isInteger(n)) return '$' + n.toLocaleString('en-US')
  let s = n.toFixed(3).replace(/0+$/, '')
  const dot = s.indexOf('.')
  if (s.length - dot - 1 < 2) s = s.padEnd(dot + 3, '0')
  return '$' + s
}

/** Relative time for a unix timestamp (seconds). */
export function timeAgo(unix: number | null | undefined, lang = 'en'): string {
  if (!unix) return '–'
  const s = Math.round(unix - Date.now() / 1000)
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto', style: 'short' })
  const a = Math.abs(s)
  if (a < 60) return rtf.format(s, 'second')
  if (a < 3600) return rtf.format(Math.round(s / 60), 'minute')
  if (a < 86400) return rtf.format(Math.round(s / 3600), 'hour')
  return rtf.format(Math.round(s / 86400), 'day')
}

export function dateTime(unix: number | null | undefined, lang = 'en'): string {
  if (!unix) return '–'
  return new Date(unix * 1000).toLocaleString(lang, { dateStyle: 'medium', timeStyle: 'short' })
}

export function duration(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d) return `${d}d ${h}h`
  if (h) return `${h}h ${m}m`
  return `${m}m`
}

export function maskEmail(email: string | undefined | null): string {
  if (!email) return ''
  const [user, domain] = email.split('@')
  if (!domain) return email.length > 4 ? email.slice(0, 2) + '•••' + email.slice(-1) : '•••'
  const u = user.length <= 2 ? user[0] + '•' : user.slice(0, 2) + '•••' + user.slice(-1)
  return `${u}@${domain}`
}

export async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  }
}
