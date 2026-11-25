/** Client-side CSV export (RFC 4180 quoting, spreadsheet formula injection guarded). */

export type CsvValue = string | number | boolean | null | undefined

/**
 * One CSV cell. Text that a spreadsheet would run as a formula (leading =, +,
 * -, @, tab or carriage return) gets a leading single quote; cells with a
 * comma, quote or line break are quoted with inner quotes doubled. Numbers are
 * written as they are.
 */
export function csvCell(v: CsvValue): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'number') return Number.isFinite(v) ? String(v) : ''
  let s = String(v)
  if (/^[=+\-@\t\r]/.test(s)) s = "'" + s
  return /[",\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s
}

export function toCsv(header: string[], rows: CsvValue[][]): string {
  return [header, ...rows].map((r) => r.map(csvCell).join(',')).join('\r\n') + '\r\n'
}

/** Saves text as a file through a temporary object URL. */
export function downloadCsv(filename: string, header: string[], rows: CsvValue[][]) {
  const blob = new Blob([toCsv(header, rows)], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
