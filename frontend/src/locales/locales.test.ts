import { describe, expect, it } from 'vitest'
import { en } from './en'
import { vi as viDict } from './vi'
import { zh } from './zh'

// Locale integrity: English is the source of truth. A translation may lag (the
// console falls back to English for a missing key), but it must never carry a
// key English doesn't have, an empty string, or different {placeholders} —
// those would show up as raw "{n}" text or lose a value in the UI.

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()

const translations = { zh, vi: viDict } as Record<string, Record<string, string | undefined>>
const english = en as Record<string, string>

describe('English messages', () => {
  it('are strings', () => {
    // A few are intentionally empty (e.g. a badge with nothing to say).
    expect(Object.entries(english).filter(([, v]) => typeof v !== 'string')).toEqual([])
  })

  it('use well-formed {placeholders}', () => {
    // An unbalanced brace usually means a typo like "{count" that t() would never replace.
    const bad = Object.entries(english).filter(([, v]) => (v.match(/\{/g)?.length ?? 0) !== (v.match(/\}/g)?.length ?? 0))
    expect(bad).toEqual([])
  })
})

describe.each(Object.keys(translations))('%s messages', (lang) => {
  const dict = translations[lang]

  it('only use keys that exist in English', () => {
    expect(Object.keys(dict).filter((k) => !(k in english))).toEqual([])
  })

  it('are non-empty strings where English is non-empty', () => {
    const bad = Object.entries(dict).filter(([k, v]) => typeof v !== 'string' || (v.trim() === '' && english[k] !== ''))
    expect(bad).toEqual([])
  })

  it('keep exactly the placeholders of the English text', () => {
    const mismatched = Object.entries(dict)
      .filter(([k, v]) => k in english && typeof v === 'string')
      .filter(([k, v]) => placeholders(v as string).join() !== placeholders(english[k]).join())
      .map(([k, v]) => ({ key: k, en: english[k], [lang]: v }))
    expect(mismatched).toEqual([])
  })

  it('resolve every English key (translated or falling back)', () => {
    // Mirrors t(): dict[key] ?? en[key]. Reports coverage so a lagging
    // translation is visible in the test output without failing the run.
    const missing = Object.keys(english).filter((k) => dict[k] === undefined)
    for (const k of Object.keys(english)) expect(typeof (dict[k] ?? english[k])).toBe('string')
    if (missing.length) console.info(`[locales] ${lang}: ${missing.length} of ${Object.keys(english).length} keys fall back to English`)
  })
})
