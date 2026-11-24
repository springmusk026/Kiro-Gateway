import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { en, type MessageKey } from '@/locales/en'
import { zh } from '@/locales/zh'
import { vi } from '@/locales/vi'

export type Lang = 'en' | 'zh' | 'vi'
export const LANGS: { id: Lang; label: string; short: string }[] = [
  { id: 'en', label: 'English', short: 'EN' },
  { id: 'zh', label: '中文', short: '中' },
  { id: 'vi', label: 'Tiếng Việt', short: 'VI' },
]
const dicts: Record<Lang, Partial<Record<MessageKey, string>>> = { en, zh, vi }
const KEY = 'kiro_lang'

function detect(): Lang {
  try {
    const saved = localStorage.getItem(KEY) as Lang | null
    if (saved && saved in dicts) return saved
  } catch {
    /* storage unavailable */
  }
  for (const l of navigator.languages ?? [navigator.language]) {
    const code = l.slice(0, 2).toLowerCase() as Lang
    if (code in dicts) return code
  }
  return 'en'
}

export type TFn = (key: MessageKey, vars?: Record<string, string | number>) => string

const Ctx = createContext<{ lang: Lang; setLang: (l: Lang) => void; t: TFn }>({
  lang: 'en',
  setLang: () => {},
  t: (k) => en[k] ?? k,
})

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => (typeof window === 'undefined' ? 'en' : detect()))
  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])
  const setLang = useCallback((l: Lang) => {
    setLangState(l)
    try {
      localStorage.setItem(KEY, l)
    } catch {
      /* storage unavailable */
    }
  }, [])
  const t = useCallback<TFn>(
    (key, vars) => {
      let s = dicts[lang][key] ?? en[key] ?? key
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v))
      return s
    },
    [lang],
  )
  return <Ctx.Provider value={{ lang, setLang, t }}>{children}</Ctx.Provider>
}

export const useI18n = () => useContext(Ctx)
