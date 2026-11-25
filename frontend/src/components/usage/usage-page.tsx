import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Eye, EyeOff, KeyRound, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { EmptyState } from '@/components/app/empty-state'
import { Logo } from '@/components/app/logo'
import { UsageMeter } from '@/components/keys/usage-meter'
import { fmtMs } from '@/components/logs/history-bits'
import { StatusBadge } from '@/components/logs/log-meta'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { compact, dateTime, timeAgo } from '@/lib/format'
import { LANGS, useI18n } from '@/lib/i18n'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

/** GET /v1/usage (authenticated with the API key itself). */
interface KeyUsage {
  key: { name: string; keyMasked: string; enabled: boolean; createdAt: number; lastUsedAt?: number; expiresAt?: number; expired: boolean }
  limits: { tokenLimit: number; creditLimit: number; dailyCreditLimit: number; rpmLimit: number; allowedModels: string[] | null }
  period: { requests: number; tokens: number; credits: number }
  today: {
    day: string
    resetsAt: string
    requests: number
    inputTokens: number
    outputTokens: number
    tokens: number
    credits: number
    dailyCredits: number
  }
  timeZone: string
  history: boolean
  daily: { day: string; requests: number; errors: number; inputTokens: number; outputTokens: number; credits: number }[] | null
  recent:
    | {
        time: string
        endpoint: string
        model: string
        status: string
        httpStatus: number
        inputTokens: number
        outputTokens: number
        credits: number
        durationMs: number
      }[]
    | null
}

class UsageError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}

// The key lives in sessionStorage only: gone when the tab closes.
const KEY_STORE = 'kiro_usage_key'
function readKey(): string {
  try {
    return sessionStorage.getItem(KEY_STORE) ?? ''
  } catch {
    return ''
  }
}
function storeKey(v: string) {
  try {
    if (v) sessionStorage.setItem(KEY_STORE, v)
    else sessionStorage.removeItem(KEY_STORE)
  } catch {
    /* storage unavailable: the key is kept in memory for this visit */
  }
}

// This page is for key holders, not admins: it calls the public API with the
// key, never the admin API (so no session cookie is involved).
async function fetchUsage(key: string): Promise<KeyUsage> {
  const res = await fetch('/v1/usage', { headers: { Authorization: `Bearer ${key}` }, credentials: 'omit', cache: 'no-store' })
  const text = await res.text()
  let body: unknown
  try {
    body = JSON.parse(text)
  } catch {
    body = undefined
  }
  if (!res.ok) {
    const err = (body as { error?: { message?: string } | string } | undefined)?.error
    const msg = typeof err === 'string' ? err : (err?.message ?? `HTTP ${res.status}`)
    throw new UsageError(msg, res.status)
  }
  return body as KeyUsage
}

const fmtCredits = (n: number) => (n >= 1000 ? compact(n) : n.toFixed(n < 10 ? 2 : 1))

export function UsagePage() {
  const { t } = useI18n()
  const [key, setKey] = useState(() => (typeof window === 'undefined' ? '' : readKey()))
  const usage = useQuery({
    queryKey: ['key-usage', key],
    enabled: !!key,
    queryFn: () => fetchUsage(key),
    retry: (n, err) => n < 2 && !(err instanceof UsageError && err.status < 500),
    refetchInterval: 60_000,
  })
  const rejected = usage.error instanceof UsageError && usage.error.status === 401

  const submit = (v: string) => {
    storeKey(v)
    setKey(v)
  }
  const forget = () => submit('')

  return (
    <div className="min-h-svh bg-background">
      <TopBar />
      <main className="mx-auto w-full max-w-5xl px-4 pb-16 sm:px-6">
        {!key || rejected ? (
          <KeyForm onSubmit={submit} error={rejected ? t('usage.invalid') : ''} />
        ) : usage.isLoading ? (
          <div className="space-y-4 pt-4">
            <Skeleton className="h-16 w-full rounded-xl" />
            <div className="grid gap-4 md:grid-cols-3">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-44 rounded-xl" />
              ))}
            </div>
            <Skeleton className="h-72 w-full rounded-xl" />
          </div>
        ) : usage.isError || !usage.data ? (
          <Card className="mt-6">
            <EmptyState
              icon={KeyRound}
              title={t('common.error')}
              description={t('usage.failed', { msg: (usage.error as Error)?.message ?? '' })}
              action={
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => usage.refetch()}>
                    {t('common.retry')}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={forget}>
                    {t('usage.switchKey')}
                  </Button>
                </div>
              }
            />
          </Card>
        ) : (
          <UsageView data={usage.data} fetching={usage.isFetching} onRefresh={() => usage.refetch()} onSwitch={forget} />
        )}
      </main>
    </div>
  )
}

function TopBar() {
  const { t, lang, setLang } = useI18n()
  const { resolved, setTheme } = useTheme()
  return (
    <header className="mx-auto flex w-full max-w-5xl items-center justify-between gap-3 px-4 py-4 sm:px-6">
      <div className="flex min-w-0 items-center gap-2">
        <Logo className="size-7 shrink-0" />
        <span className="truncate font-semibold">Kiro Gateway</span>
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
    </header>
  )
}

function KeyForm({ onSubmit, error }: { onSubmit: (key: string) => void; error: string }) {
  const { t } = useI18n()
  const [value, setValue] = useState('')
  const [show, setShow] = useState(false)
  return (
    <form
      className="mx-auto mt-[8vh] w-full max-w-md animate-rise"
      onSubmit={(e) => {
        e.preventDefault()
        if (value.trim()) onSubmit(value.trim())
      }}
    >
      <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('usage.title')}</h1>
      <p className="mt-1.5 text-sm text-muted-foreground">{t('usage.subtitle')}</p>
      <div className="mt-8 space-y-2">
        <Label htmlFor="usage-key">{t('usage.keyLabel')}</Label>
        <div className="relative">
          <KeyRound className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            id="usage-key"
            type={show ? 'text' : 'password'}
            autoComplete="off"
            spellCheck={false}
            autoFocus
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t('usage.keyPlaceholder')}
            className="h-11 pl-9 pr-10 font-mono"
            aria-invalid={!!error}
          />
          <button
            type="button"
            onClick={() => setShow((s) => !s)}
            className="absolute right-2 top-1/2 grid size-7 -translate-y-1/2 place-items-center rounded-md text-muted-foreground hover:text-foreground"
            aria-label={show ? t('usage.hideKey') : t('usage.showKey')}
          >
            {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
          </button>
        </div>
        <p className={cn('min-h-5 text-sm text-destructive', !error && 'invisible')} role="alert">
          {error}
        </p>
      </div>
      <Button type="submit" className="group mt-2 h-11 w-full" disabled={!value.trim()}>
        {t('usage.show')}
        <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
      </Button>
      <p className="mt-4 text-xs leading-relaxed text-muted-foreground">{t('usage.keyNote')}</p>
    </form>
  )
}

function UsageView({ data, fetching, onRefresh, onSwitch }: { data: KeyUsage; fetching: boolean; onRefresh: () => void; onSwitch: () => void }) {
  const { t, lang } = useI18n()
  const { key, limits, period, today } = data
  const models = limits.allowedModels ?? []
  return (
    <div className="space-y-5 pt-2 stagger">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className={cn('truncate text-2xl font-semibold tracking-tight', !key.name && 'italic text-muted-foreground')}>{key.name || t('usage.unnamed')}</h1>
            {key.expired ? (
              <Badge variant="outline" className="border-destructive/40 text-destructive">
                {t('usage.expired')}
              </Badge>
            ) : (
              <Badge variant="outline" className="border-success/30 text-success">
                {t('usage.active')}
              </Badge>
            )}
          </div>
          <code className="mt-1 block truncate font-mono text-[12.5px] text-muted-foreground">{key.keyMasked}</code>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={onRefresh} disabled={fetching}>
            <RefreshCw className={cn(fetching && 'animate-spin')} />
            {t('usage.refresh')}
          </Button>
          <Button variant="ghost" size="sm" onClick={onSwitch}>
            {t('usage.switchKey')}
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card className="min-w-0 gap-4">
          <CardHeader>
            <CardTitle>{t('usage.today')}</CardTitle>
            <CardDescription>{t('usage.todayHint', { when: dateTime(Date.parse(today.resetsAt) / 1000, lang), tz: data.timeZone })}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Numbers
              items={[
                [t('usage.requests'), compact(today.requests)],
                [t('usage.tokens'), compact(today.tokens)],
                [t('usage.credits'), fmtCredits(today.credits)],
              ]}
            />
            {limits.dailyCreditLimit > 0 && (
              <UsageMeter label={t('usage.dailyCredits')} used={today.dailyCredits} limit={limits.dailyCreditLimit} format={fmtCredits} />
            )}
          </CardContent>
        </Card>
        <Card className="min-w-0 gap-4">
          <CardHeader>
            <CardTitle>{t('usage.period')}</CardTitle>
            <CardDescription>{t('usage.periodHint')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <UsageMeter label={t('usage.tokens')} used={period.tokens} limit={limits.tokenLimit} format={compact} />
            <UsageMeter label={t('usage.credits')} used={period.credits} limit={limits.creditLimit} format={fmtCredits} />
            <p className="flex items-baseline justify-between text-xs">
              <span className="text-muted-foreground">{t('usage.requests')}</span>
              <span className="font-medium tabular">{period.requests.toLocaleString(lang)}</span>
            </p>
          </CardContent>
        </Card>
        <Card className="min-w-0 gap-4">
          <CardHeader>
            <CardTitle>{t('usage.limits')}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted-foreground">{t('usage.rpm')}</dt>
              <dd className="text-right tabular">{limits.rpmLimit > 0 ? limits.rpmLimit : t('usage.unlimited')}</dd>
              <dt className="text-muted-foreground">{t('usage.dailyCredits')}</dt>
              <dd className="text-right tabular">{limits.dailyCreditLimit > 0 ? limits.dailyCreditLimit : t('usage.unlimited')}</dd>
              <dt className="text-muted-foreground">{t('usage.expires')}</dt>
              <dd className={cn('text-right', key.expired && 'text-destructive')}>{key.expiresAt ? dateTime(key.expiresAt, lang) : t('usage.never')}</dd>
              <dt className="text-muted-foreground">{t('usage.models')}</dt>
              <dd className="flex min-w-0 flex-wrap justify-end gap-1">
                {models.length ? (
                  models.map((m) => (
                    <Badge key={m} variant="secondary" className="max-w-full truncate font-mono text-[11px]">
                      {m}
                    </Badge>
                  ))
                ) : (
                  <span>{t('usage.allModels')}</span>
                )}
              </dd>
            </dl>
          </CardContent>
        </Card>
      </div>

      {data.history ? (
        <>
          <DailyChart data={data} />
          <RecentCard data={data} />
        </>
      ) : (
        <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">{t('usage.noHistory')}</p>
      )}
    </div>
  )
}

function Numbers({ items }: { items: [string, string][] }) {
  return (
    <dl className="grid grid-cols-3 gap-3">
      {items.map(([k, v]) => (
        <div key={k} className="min-w-0">
          <dt className="truncate text-xs text-muted-foreground">{k}</dt>
          <dd className="mt-0.5 text-xl font-semibold tracking-tight tabular">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

type Metric = 'tokens' | 'credits' | 'requests'

function DailyChart({ data }: { data: KeyUsage }) {
  const { t, lang } = useI18n()
  const [metric, setMetric] = useState<Metric>('tokens')
  const rows = useMemo(
    () =>
      (data.daily ?? []).map((d) => {
        const date = new Date(d.day + 'T12:00:00Z')
        return {
          label: date.toLocaleDateString(lang, { month: 'short', day: 'numeric', timeZone: 'UTC' }),
          tokens: d.inputTokens + d.outputTokens,
          credits: +d.credits.toFixed(4),
          requests: d.requests,
          d,
        }
      }),
    [data.daily, lang],
  )
  const fmt = metric === 'credits' ? fmtCredits : compact
  return (
    <Card className="min-w-0 gap-4">
      <CardHeader>
        <CardTitle>{t('usage.chart')}</CardTitle>
        <CardDescription>{t('usage.chartHint', { tz: data.timeZone })}</CardDescription>
        <CardAction>
          <ToggleGroup type="single" variant="outline" size="sm" value={metric} onValueChange={(v) => v && setMetric(v as Metric)} aria-label={t('usage.chart')}>
            <ToggleGroupItem value="tokens" className="px-2.5 text-xs">
              {t('usage.tokens')}
            </ToggleGroupItem>
            <ToggleGroupItem value="credits" className="px-2.5 text-xs">
              {t('usage.credits')}
            </ToggleGroupItem>
            <ToggleGroupItem value="requests" className="px-2.5 text-xs">
              {t('usage.requests')}
            </ToggleGroupItem>
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={rows} margin={{ top: 6, right: 6, left: 0, bottom: 0 }}>
              <CartesianGrid vertical={false} stroke="var(--border)" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={16} tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }} />
              <YAxis width={44} tickLine={false} axisLine={false} tickFormatter={(v: number) => fmt(v)} tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }} />
              <Tooltip
                cursor={{ fill: 'var(--muted)', opacity: 0.5 }}
                content={({ active, payload }) => {
                  const r = payload?.[0]?.payload as (typeof rows)[number] | undefined
                  if (!active || !r) return null
                  return (
                    <div className="min-w-[160px] rounded-lg border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
                      <p className="mb-1.5 text-muted-foreground">{r.label}</p>
                      <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5 tabular">
                        <dt className="text-muted-foreground">{t('usage.requests')}</dt>
                        <dd className="text-right font-semibold">{r.requests.toLocaleString(lang)}</dd>
                        <dt className="text-muted-foreground">{t('usage.tokens')}</dt>
                        <dd className="text-right font-semibold">
                          {compact(r.d.inputTokens)} / {compact(r.d.outputTokens)}
                        </dd>
                        <dt className="text-muted-foreground">{t('usage.credits')}</dt>
                        <dd className="text-right font-semibold">{fmtCredits(r.credits)}</dd>
                      </dl>
                    </div>
                  )
                }}
              />
              <Bar dataKey={metric} fill="var(--chart-1)" radius={[4, 4, 0, 0]} maxBarSize={36} isAnimationActive={false} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </CardContent>
    </Card>
  )
}

function RecentCard({ data }: { data: KeyUsage }) {
  const { t, lang } = useI18n()
  const recent = data.recent ?? []
  return (
    <Card className="min-w-0 gap-0 overflow-hidden pb-0">
      <CardHeader className="pb-4">
        <CardTitle>{t('usage.recent')}</CardTitle>
        <CardDescription>{t('usage.recentHint')}</CardDescription>
      </CardHeader>
      {recent.length === 0 ? (
        <EmptyState icon={KeyRound} title={t('usage.noRecent')} />
      ) : (
        <Table className="min-w-[560px] text-[13px]">
          <TableHeader>
            <TableRow className="bg-muted/30 text-xs hover:bg-muted/30">
              <TableHead className="pl-6">{t('usage.colTime')}</TableHead>
              <TableHead>{t('usage.colModel')}</TableHead>
              <TableHead>{t('usage.colStatus')}</TableHead>
              <TableHead className="text-right">{t('usage.colTokens')}</TableHead>
              <TableHead className="pr-6 text-right">{t('usage.colDuration')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {recent.map((r, i) => (
              <TableRow key={i} className="h-11">
                <TableCell className="pl-6 tabular text-muted-foreground" title={new Date(r.time).toLocaleString(lang)}>
                  {timeAgo(Date.parse(r.time) / 1000, lang)}
                </TableCell>
                <TableCell className="max-w-[220px] truncate font-mono text-[12.5px]">{r.model || '–'}</TableCell>
                <TableCell>
                  <StatusBadge log={r} />
                </TableCell>
                <TableCell className="text-right tabular">
                  {compact(r.inputTokens)}
                  <span className="text-muted-foreground"> / </span>
                  {compact(r.outputTokens)}
                </TableCell>
                <TableCell className="pr-6 text-right tabular">{r.durationMs ? fmtMs(r.durationMs) : '–'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Card>
  )
}
