import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { MessageKey } from '@/locales/en'
import { I18nProvider, LANGS, useI18n, type Lang } from './i18n'

// A deliberately incomplete Chinese dictionary, so falling back to English is
// tested independently of how complete the real translation is.
vi.mock('@/locales/zh', () => ({
  zh: {
    'nav.overview': '概览',
    'pager.range': '{from}–{to}，共 {total}',
  },
}))

let api: ReturnType<typeof useI18n>

function Probe({ k, vars }: { k: MessageKey; vars?: Record<string, string | number> }) {
  api = useI18n()
  return <span data-testid="out">{api.t(k, vars)}</span>
}

function renderProbe(k: MessageKey, vars?: Record<string, string | number>) {
  return render(
    <I18nProvider>
      <Probe k={k} vars={vars} />
    </I18nProvider>,
  )
}

const out = () => screen.getByTestId('out').textContent

afterEach(() => {
  document.documentElement.lang = ''
})

describe('i18n', () => {
  it('lists the three console languages', () => {
    expect(LANGS.map((l) => l.id)).toEqual(['en', 'zh', 'vi'])
  })

  it('uses English by default in an English browser', () => {
    renderProbe('nav.overview')
    expect(out()).toBe('Overview')
    expect(api.lang).toBe('en')
  })

  it('picks the saved language and sets <html lang>', () => {
    localStorage.setItem('kiro_lang', 'zh')
    renderProbe('nav.overview')
    expect(out()).toBe('概览')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('ignores an unknown saved language', () => {
    localStorage.setItem('kiro_lang', 'xx')
    renderProbe('nav.overview')
    expect(api.lang).toBe('en')
  })

  it('detects the browser language when nothing is saved', () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['vi-VN', 'en'])
    renderProbe('nav.overview')
    expect(api.lang).toBe('vi')
  })

  it('falls back to English for a key the language does not translate', () => {
    localStorage.setItem('kiro_lang', 'zh')
    renderProbe('nav.settings')
    expect(out()).toBe('Settings')
  })

  it('returns the key itself for an unknown key', () => {
    renderProbe('does.not.exist' as MessageKey)
    expect(out()).toBe('does.not.exist')
  })

  it('interpolates every occurrence of each placeholder', () => {
    renderProbe('pager.range', { from: 1, to: 25, total: '1,000' })
    expect(out()).toBe('1–25 of 1,000')
  })

  it('interpolates translated strings and leaves unknown placeholders alone', () => {
    localStorage.setItem('kiro_lang', 'zh')
    renderProbe('pager.range', { from: 26, to: 50 })
    expect(out()).toBe('26–50，共 {total}')
  })

  it('switches language at runtime and remembers it', () => {
    renderProbe('nav.overview')
    act(() => api.setLang('zh' as Lang))
    expect(out()).toBe('概览')
    expect(localStorage.getItem('kiro_lang')).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('works outside a provider with the English defaults', () => {
    render(<Probe k="nav.models" />)
    expect(out()).toBe('Models')
  })
})
