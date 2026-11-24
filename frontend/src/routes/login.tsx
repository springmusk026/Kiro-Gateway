import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ArrowRight, Eye, EyeOff, Gauge, Loader2, Lock, Route as RouteIcon, ShieldCheck, Terminal } from 'lucide-react'
import { useState } from 'react'
import { Logo } from '@/components/app/logo'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuth } from '@/lib/auth'
import { LANGS, useI18n } from '@/lib/i18n'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/login')({ component: LoginPage })

function LoginPage() {
  const { t, lang, setLang } = useI18n()
  const { resolved, setTheme } = useTheme()
  const { login } = useAuth()
  const navigate = useNavigate()
  const [pw, setPw] = useState('')
  const [show, setShow] = useState(false)
  const [remember, setRemember] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [shake, setShake] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pw || busy) return
    setBusy(true)
    setError('')
    const r = await login(pw, remember)
    setBusy(false)
    if (r === 'ok') {
      navigate({ to: '/' })
      return
    }
    setError(r === 'locked' ? t('login.locked') : r === 'wrong' ? t('login.wrong') : t('login.unreachable'))
    setShake(true)
    setTimeout(() => setShake(false), 450)
  }

  const features = [
    { icon: RouteIcon, text: t('login.f1') },
    { icon: ShieldCheck, text: t('login.f2') },
    { icon: Terminal, text: t('login.f3') },
  ]

  return (
    <div className="grid min-h-svh lg:grid-cols-[1.05fr_1fr]">
      <section className="relative hidden overflow-hidden bg-[oklch(0.17_0.04_280)] p-12 text-white lg:flex lg:flex-col">
        <div className="pointer-events-none absolute inset-0">
          <div className="absolute -left-24 -top-24 size-[34rem] rounded-full bg-[oklch(0.55_0.22_285)] opacity-40 blur-3xl" />
          <div className="absolute -bottom-32 right-[-6rem] size-[30rem] rounded-full bg-[oklch(0.6_0.16_220)] opacity-30 blur-3xl" />
          <svg className="absolute inset-0 size-full opacity-[0.07]" aria-hidden>
            <defs>
              <pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse">
                <path d="M32 0H0V32" fill="none" stroke="white" strokeWidth="1" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#grid)" />
          </svg>
        </div>
        <div className="relative flex items-center gap-3">
          <Logo className="size-9" />
          <span className="text-lg font-semibold tracking-tight">Kiro-Go</span>
        </div>
        <div className="relative mt-auto max-w-md">
          <h2 className="text-4xl font-semibold leading-tight tracking-tight">{t('login.heroTitle')}</h2>
          <p className="mt-4 text-[15px] leading-relaxed text-white/70">{t('login.heroText')}</p>
          <ul className="mt-8 space-y-3">
            {features.map((f, i) => (
              <li key={i} className="flex items-center gap-3 text-sm text-white/85 animate-rise" style={{ animationDelay: `${150 + i * 80}ms` }}>
                <span className="grid size-8 place-items-center rounded-lg bg-white/10 ring-1 ring-white/15">
                  <f.icon className="size-4" />
                </span>
                {f.text}
              </li>
            ))}
          </ul>
        </div>
        <div className="relative mt-auto flex items-center gap-2 pt-12 text-xs text-white/50">
          <Gauge className="size-3.5" /> OpenAI · Anthropic · Responses API
        </div>
      </section>

      <section className="flex flex-col">
        <div className="flex items-center justify-between p-5">
          <div className="flex items-center gap-2 lg:invisible">
            <Logo className="size-7" />
            <span className="font-semibold">Kiro-Go</span>
          </div>
          <div className="flex items-center gap-1 rounded-lg border bg-card p-0.5 text-xs">
            {LANGS.map((l) => (
              <button
                key={l.id}
                onClick={() => setLang(l.id)}
                className={cn('rounded-md px-2.5 py-1 font-medium transition-colors', lang === l.id ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground')}
              >
                {l.short}
              </button>
            ))}
            <button
              onClick={() => setTheme(resolved === 'dark' ? 'light' : 'dark')}
              className="rounded-md px-2 py-1 text-muted-foreground hover:text-foreground"
              aria-label={t('common.theme')}
            >
              {resolved === 'dark' ? '☀︎' : '☾'}
            </button>
          </div>
        </div>
        <div className="grid flex-1 place-items-center px-6 pb-16">
          <form onSubmit={submit} className={cn('w-full max-w-sm animate-rise', shake && 'animate-[shake_0.45s]')}>
            <h1 className="text-3xl font-semibold tracking-tight">{t('login.title')}</h1>
            <p className="mt-1.5 text-muted-foreground">{t('login.subtitle')}</p>
            <div className="mt-8 space-y-2">
              <Label htmlFor="pw">{t('login.password')}</Label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="pw"
                  type={show ? 'text' : 'password'}
                  autoComplete="current-password"
                  autoFocus
                  value={pw}
                  onChange={(e) => setPw(e.target.value)}
                  placeholder={t('login.placeholder')}
                  className="h-11 pl-9 pr-10"
                  aria-invalid={!!error}
                />
                <button
                  type="button"
                  onClick={() => setShow((s) => !s)}
                  className="absolute right-2 top-1/2 grid size-7 -translate-y-1/2 place-items-center rounded-md text-muted-foreground hover:text-foreground"
                  aria-label={show ? 'Hide password' : 'Show password'}
                >
                  {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
              <p className={cn('h-5 text-sm text-destructive transition-opacity', error ? 'opacity-100' : 'opacity-0')}>{error}</p>
            </div>
            <label className="mt-1 flex cursor-pointer items-center gap-2 text-sm text-muted-foreground">
              <Checkbox checked={remember} onCheckedChange={(v) => setRemember(v === true)} />
              {t('login.remember')}
            </label>
            <Button type="submit" className="group mt-6 h-11 w-full text-[15px]" disabled={!pw || busy}>
              {busy ? <Loader2 className="size-4 animate-spin" /> : null}
              {t('login.submit')}
              {!busy && <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />}
            </Button>
            <p className="mt-6 text-xs leading-relaxed text-muted-foreground">{t('login.hint')}</p>
          </form>
        </div>
      </section>
    </div>
  )
}
