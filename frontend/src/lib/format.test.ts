import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { compact, copyToClipboard, dateTime, duration, fixed, maskEmail, percent, timeAgo, usd, usdRate } from './format'

describe('compact', () => {
  it.each([
    [0, '0'],
    [999, '999'],
    [1000, '1K'],
    [1500, '1.5K'],
    [-2500, '-2.5K'],
    [1_000_000, '1M'],
    [12_345_678, '12.3M'],
    [2_000_000_000, '2B'],
  ])('%d -> %s', (n, s) => expect(compact(n)).toBe(s))

  it('shows a dash for missing values', () => {
    expect(compact(null)).toBe('–')
    expect(compact(undefined)).toBe('–')
    expect(compact(NaN)).toBe('–')
  })
})

describe('usd', () => {
  it.each([
    [0, '$0.00'],
    [0.0031, '$0.0031'],
    [0.00314159, '$0.003142'],
    [0.123456, '$0.1235'],
    [0.5, '$0.50'],
    [0.05, '$0.05'],
    [0.99996, '$1.00'],
    [0.00004, '<$0.0001'],
    [1, '$1.00'],
    [12.5, '$12.50'],
    [1234.567, '$1,234.57'],
    [999_999, '$999,999.00'],
    [2_500_000, '$2.5M'],
    [-0.25, '-$0.25'],
    [-42, '-$42.00'],
  ])('%d -> %s', (n, s) => expect(usd(n)).toBe(s))

  it('abbreviates from ten thousand with compact', () => {
    expect(usd(9_999.5, { compact: true })).toBe('$9,999.50')
    expect(usd(12_345, { compact: true })).toBe('$12.3K')
    expect(usd(0.0031, { compact: true })).toBe('$0.0031')
  })

  it('shows a dash for missing values', () => {
    expect(usd(null)).toBe('–')
    expect(usd(undefined)).toBe('–')
    expect(usd(NaN)).toBe('–')
    expect(usd(Infinity)).toBe('–')
  })
})

describe('usdRate', () => {
  it.each([
    [3, '$3'],
    [15, '$15'],
    [0.3, '$0.30'],
    [3.75, '$3.75'],
    [0.075, '$0.075'],
    [1250, '$1,250'],
  ])('%d -> %s', (n, s) => expect(usdRate(n)).toBe(s))
})

describe('fixed and percent', () => {
  it('formats with the given digits', () => {
    expect(fixed(1.25)).toBe('1.3')
    expect(fixed(2, 2)).toBe('2.00')
    expect(fixed(null)).toBe('–')
    expect(fixed(NaN)).toBe('–')
  })

  it('computes a one-decimal percentage and guards a zero total', () => {
    expect(percent(1, 3)).toBe('33.3%')
    expect(percent(5, 5)).toBe('100.0%')
    expect(percent(1, 0)).toBe('–')
  })
})

describe('duration', () => {
  it.each([
    [0, '0m'],
    [59, '0m'],
    [61, '1m'],
    [3600, '1h 0m'],
    [3 * 3600 + 25 * 60, '3h 25m'],
    [86400, '1d 0h'],
    [2 * 86400 + 5 * 3600 + 59, '2d 5h'],
  ])('%d s -> %s', (sec, s) => expect(duration(sec)).toBe(s))
})

describe('timeAgo and dateTime', () => {
  const now = new Date('2026-03-01T12:00:00Z').getTime()
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
  })
  afterEach(() => vi.useRealTimers())

  const ago = (s: number) => now / 1000 - s

  it('picks the right unit', () => {
    expect(timeAgo(ago(0))).toBe('now')
    expect(timeAgo(ago(30))).toBe('30 sec. ago')
    expect(timeAgo(ago(120))).toBe('2 min. ago')
    expect(timeAgo(ago(3 * 3600))).toBe('3 hr. ago')
    expect(timeAgo(ago(86400))).toBe('yesterday')
    expect(timeAgo(ago(5 * 86400))).toBe('5 days ago')
    expect(timeAgo(now / 1000 + 120)).toBe('in 2 min.')
  })

  it('localises', () => {
    expect(timeAgo(ago(120), 'zh')).toBe('2分钟前')
  })

  it('shows a dash for a missing timestamp', () => {
    expect(timeAgo(0)).toBe('–')
    expect(timeAgo(null)).toBe('–')
    expect(dateTime(undefined)).toBe('–')
  })

  it('formats a date and time', () => {
    expect(dateTime(now / 1000, 'en')).toMatch(/Mar 1, 2026/)
  })
})

describe('maskEmail', () => {
  it.each([
    ['alice@example.com', 'al•••e@example.com'],
    ['ab@example.com', 'a•@example.com'],
    ['a@example.com', 'a•@example.com'],
    ['not-an-email', 'no•••l'],
    ['abc', '•••'],
    ['', ''],
  ])('%s -> %s', (input, masked) => expect(maskEmail(input)).toBe(masked))

  it('handles null', () => {
    expect(maskEmail(null)).toBe('')
    expect(maskEmail(undefined)).toBe('')
  })
})

describe('copyToClipboard', () => {
  it('uses the async clipboard API', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await copyToClipboard('ksk_x')
    expect(writeText).toHaveBeenCalledWith('ksk_x')
  })

  it('falls back to execCommand when the clipboard API is unavailable', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    const exec = vi.fn().mockReturnValue(true)
    Object.defineProperty(document, 'execCommand', { value: exec, configurable: true })
    await copyToClipboard('fallback')
    expect(exec).toHaveBeenCalledWith('copy')
    expect(document.querySelector('textarea')).toBeNull()
  })
})
