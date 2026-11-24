import { ChevronRight, Eye, EyeOff, Filter, Loader2, RefreshCw, ScrollText, Search, Trash2, X } from 'lucide-react'
import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { CopyButton } from '@/components/app/copy-button'
import { EmptyState } from '@/components/app/empty-state'
import { PageHeader } from '@/components/app/page-header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, maskEmail, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useAccountLookup, useClearLogs, useLogsLive } from '@/lib/queries.settings'
import type { RequestLog } from '@/lib/types'
import { cn } from '@/lib/utils'
import { ENDPOINT_LABEL, ERROR_TYPES, EndpointChip, ErrorTypeBadge, StatusBadge } from './log-meta'
import { SummaryStrip } from './summary-strip'

type Keyed = RequestLog & { key: string }
const PAGE = 100
const LIVE_KEY = 'kiro_logs_live'
const PRIVACY_KEY = 'privacyMode'

function readBool(key: string, def: boolean) {
  try {
    const v = localStorage.getItem(key)
    return v == null ? def : v === 'true'
  } catch {
    return def
  }
}
function writeBool(key: string, v: boolean) {
  try {
    localStorage.setItem(key, String(v))
  } catch {
    /* storage unavailable */
  }
}

/** Logs have no id; derive a stable key, counting duplicates from the oldest end. */
function withKeys(logs: RequestLog[]): Keyed[] {
  const seen = new Map<string, number>()
  const out: Keyed[] = new Array(logs.length)
  for (let i = logs.length - 1; i >= 0; i--) {
    const l = logs[i]
    const base = `${l.time}|${l.endpoint}|${l.model}|${l.accountId}|${l.status}|${l.tokens}|${l.duration}|${l.error.length}`
    const n = seen.get(base) ?? 0
    seen.set(base, n + 1)
    out[i] = { ...l, key: `${base}#${n}` }
  }
  return out
}

const fmtMs = (ms: number) => (ms >= 10_000 ? `${(ms / 1000).toFixed(1)}s` : ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`)

function exactTime(unix: number, lang: string) {
  return new Date(unix * 1000).toLocaleString(lang, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function LogsPage() {
  const { t, lang } = useI18n()
  const confirm = useConfirm()
  const [live, setLiveState] = useState(() => (typeof window === 'undefined' ? true : readBool(LIVE_KEY, true)))
  const [privacy, setPrivacyState] = useState(() => (typeof window === 'undefined' ? true : readBool(PRIVACY_KEY, true)))
  const logs = useLogsLive(live ? 5_000 : false)
  const accounts = useAccountLookup()
  const clear = useClearLogs()
  const [status, setStatus] = useState('all')
  const [endpoint, setEndpoint] = useState('all')
  const [errType, setErrType] = useState('all')
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [limit, setLimit] = useState(PAGE)
  const [fresh, setFresh] = useState<Set<string>>(new Set())
  const known = useRef<Set<string> | null>(null)
  const [, tick] = useState(0)

  const setLive = (v: boolean) => {
    setLiveState(v)
    writeBool(LIVE_KEY, v)
  }
  const setPrivacy = (v: boolean) => {
    setPrivacyState(v)
    writeBool(PRIVACY_KEY, v)
  }

  // Re-render relative times every 15s.
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 15_000)
    return () => clearInterval(id)
  }, [])

  const rows = useMemo(() => withKeys(logs.data ?? []), [logs.data])

  // Highlight rows that arrived since the previous fetch.
  useEffect(() => {
    if (!logs.data) return
    const keys = new Set(rows.map((r) => r.key))
    if (known.current) {
      const added = rows.filter((r) => !known.current!.has(r.key)).map((r) => r.key)
      if (added.length) {
        setFresh(new Set(added))
        const id = setTimeout(() => setFresh(new Set()), 2600)
        known.current = keys
        return () => clearTimeout(id)
      }
    }
    known.current = keys
  }, [rows, logs.data])

  const accountLabel = (id: string) => {
    if (!id) return '–'
    const email = accounts.data?.[id]
    if (email) return privacy ? maskEmail(email) : email
    return id.slice(0, 8)
  }

  const endpoints = useMemo(() => Array.from(new Set(rows.map((r) => r.endpoint).filter(Boolean))).sort(), [rows])

  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase()
    return rows.filter((r) => {
      if (status !== 'all' && r.status !== status) return false
      if (endpoint !== 'all' && r.endpoint !== endpoint) return false
      if (errType !== 'all' && (r.errorType || (r.status === 'error' ? 'unknown' : '')) !== errType) return false
      if (s) {
        const email = accounts.data?.[r.accountId] ?? ''
        const hay = `${r.model} ${r.accountId} ${email} ${r.error}`.toLowerCase()
        if (!hay.includes(s)) return false
      }
      return true
    })
  }, [rows, status, endpoint, errType, search, accounts.data])

  useEffect(() => setLimit(PAGE), [status, endpoint, errType, search])

  const filtersActive = status !== 'all' || endpoint !== 'all' || errType !== 'all' || search.trim() !== ''
  const resetFilters = () => {
    setStatus('all')
    setEndpoint('all')
    setErrType('all')
    setSearch('')
  }

  const onClear = async () => {
    const ok = await confirm({
      title: t('logs.clearTitle'),
      description: t('logs.clearBody'),
      confirmLabel: t('logs.clear'),
      destructive: true,
    })
    if (!ok) return
    try {
      await clear.mutateAsync()
      known.current = new Set()
      toast.success(t('logs.cleared'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('nav.logs')}
        description={t('page.logs')}
        actions={
          <>
            <label
              className={cn(
                'flex h-9 cursor-pointer items-center gap-2 rounded-lg border bg-card px-3 text-sm',
                live && 'border-primary/30',
              )}
            >
              <span
                className={cn(
                  'size-2 rounded-full',
                  live ? 'animate-pulse-ring bg-primary text-primary' : 'bg-muted-foreground/40',
                )}
                aria-hidden
              />
              <span className={cn('font-medium', !live && 'text-muted-foreground')}>{live ? t('logs.live') : t('logs.paused')}</span>
              <Switch checked={live} onCheckedChange={setLive} aria-label={t('logs.autoRefresh')} className="ml-1" />
            </label>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => logs.refetch()}
                  disabled={logs.isFetching}
                  aria-label={t('common.refresh')}
                >
                  <RefreshCw className={cn(logs.isFetching && 'animate-spin')} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('common.refresh')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => setPrivacy(!privacy)}
                  aria-label={privacy ? t('logs.showEmails') : t('logs.hideEmails')}
                  aria-pressed={!privacy}
                >
                  {privacy ? <EyeOff /> : <Eye />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{privacy ? t('logs.showEmails') : t('logs.hideEmails')}</TooltipContent>
            </Tooltip>
            <Button
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={onClear}
              disabled={clear.isPending || rows.length === 0}
            >
              {clear.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
              {t('logs.clear')}
            </Button>
          </>
        }
      />

      <SummaryStrip logs={filtered} loading={logs.isLoading} filtered={filtersActive} total={rows.length} />

      <Card className="gap-0 overflow-hidden py-0 shadow-xs">
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-[200px] flex-1">
            <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder={t('logs.searchPlaceholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              aria-label={t('logs.searchPlaceholder')}
            />
          </div>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-[130px]" aria-label={t('logs.colStatus')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('logs.allStatuses')}</SelectItem>
              <SelectItem value="success">{t('logs.success')}</SelectItem>
              <SelectItem value="error">{t('logs.error')}</SelectItem>
            </SelectContent>
          </Select>
          <Select value={endpoint} onValueChange={setEndpoint}>
            <SelectTrigger className="w-[150px]" aria-label={t('logs.colEndpoint')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('logs.allEndpoints')}</SelectItem>
              {endpoints.map((e) => (
                <SelectItem key={e} value={e}>
                  {ENDPOINT_LABEL[e] ? t(ENDPOINT_LABEL[e]) : e}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={errType} onValueChange={setErrType}>
            <SelectTrigger className="w-[160px]" aria-label={t('logs.colErrorType')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('logs.allErrorTypes')}</SelectItem>
              {ERROR_TYPES.map((e) => (
                <SelectItem key={e.id} value={e.id}>
                  <e.icon />
                  {t(e.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={resetFilters}>
              <X />
              {t('logs.resetFilters')}
            </Button>
          )}
        </div>

        {logs.isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : logs.isError ? (
          <EmptyState
            icon={ScrollText}
            title={t('common.error')}
            description={(logs.error as Error).message}
            action={
              <Button size="sm" variant="outline" onClick={() => logs.refetch()}>
                {t('common.retry')}
              </Button>
            }
          />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={ScrollText}
            title={t('logs.emptyTitle')}
            description={t('logs.emptyDesc')}
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Filter}
            title={t('logs.noMatchTitle')}
            description={t('logs.noMatchDesc')}
            action={
              <Button size="sm" variant="outline" onClick={resetFilters}>
                {t('logs.resetFilters')}
              </Button>
            }
          />
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/30 hover:bg-muted/30">
                  <TableHead className="w-8 pl-3" />
                  <TableHead>{t('logs.colTime')}</TableHead>
                  <TableHead>{t('logs.colStatus')}</TableHead>
                  <TableHead>{t('logs.colEndpoint')}</TableHead>
                  <TableHead>{t('logs.colModel')}</TableHead>
                  <TableHead>{t('logs.colAccount')}</TableHead>
                  <TableHead className="text-right">{t('logs.colTokens')}</TableHead>
                  <TableHead className="text-right">{t('logs.colCredits')}</TableHead>
                  <TableHead className="pr-4 text-right">{t('logs.colDuration')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.slice(0, limit).map((r) => {
                  const open = expanded === r.key
                  return (
                    <Fragment key={r.key}>
                      <TableRow
                        tabIndex={0}
                        aria-expanded={open}
                        onClick={() => setExpanded(open ? null : r.key)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault()
                            setExpanded(open ? null : r.key)
                          }
                        }}
                        className={cn(
                          'h-11 cursor-pointer transition-colors duration-700 focus-visible:bg-muted/50 focus-visible:outline-none',
                          fresh.has(r.key) && 'bg-primary/[0.07]',
                          open && 'border-b-0',
                        )}
                      >
                        <TableCell className="pl-3">
                          <ChevronRight
                            className={cn('size-4 text-muted-foreground transition-transform', open && 'rotate-90')}
                          />
                        </TableCell>
                        <TableCell>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="tabular text-muted-foreground">{timeAgo(r.time, lang)}</span>
                            </TooltipTrigger>
                            <TooltipContent>{exactTime(r.time, lang)}</TooltipContent>
                          </Tooltip>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1.5">
                            <StatusBadge log={r} />
                            {r.status === 'error' && <ErrorTypeBadge type={r.errorType || 'unknown'} />}
                          </div>
                        </TableCell>
                        <TableCell>
                          <EndpointChip endpoint={r.endpoint} />
                        </TableCell>
                        <TableCell className="max-w-[260px]">
                          <span className="block truncate font-mono text-[12.5px]">{r.model || '–'}</span>
                          {r.status === 'error' && r.error && (
                            <span className="block truncate text-xs text-destructive/80">{r.error}</span>
                          )}
                        </TableCell>
                        <TableCell className="max-w-[220px] truncate text-muted-foreground">{accountLabel(r.accountId)}</TableCell>
                        <TableCell className="text-right tabular">{r.tokens ? compact(r.tokens) : '–'}</TableCell>
                        <TableCell className="text-right tabular">{r.credits ? r.credits.toFixed(3) : '–'}</TableCell>
                        <TableCell
                          className={cn(
                            'pr-4 text-right tabular',
                            r.duration >= 30_000 && 'text-warning',
                          )}
                        >
                          {r.duration ? fmtMs(r.duration) : '–'}
                        </TableCell>
                      </TableRow>
                      {open && (
                        <TableRow className="bg-muted/20 hover:bg-muted/20">
                          <TableCell colSpan={9} className="whitespace-normal px-4 pb-4 pt-1">
                            <LogDetail log={r} account={accountLabel(r.accountId)} />
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                })}
              </TableBody>
            </Table>
            <div className="flex flex-wrap items-center justify-between gap-2 border-t px-4 py-2.5 text-xs text-muted-foreground">
              <span className="tabular">
                {t('logs.showing', { shown: Math.min(limit, filtered.length), total: filtered.length })} · {t('logs.ringNote')}
              </span>
              {filtered.length > limit && (
                <Button variant="ghost" size="sm" onClick={() => setLimit((l) => l + PAGE)}>
                  {t('logs.showMore')}
                </Button>
              )}
            </div>
          </>
        )}
      </Card>
    </div>
  )
}

function LogDetail({ log, account }: { log: Keyed; account: string }) {
  const { t, lang } = useI18n()
  const meta = ERROR_TYPES.find((e) => e.id === (log.errorType || 'unknown'))
  const fields: [string, string][] = [
    [t('logs.colTime'), exactTime(log.time, lang)],
    [t('logs.colEndpoint'), log.endpoint || '–'],
    [t('logs.colModel'), log.model || '–'],
    [t('logs.colAccount'), account],
    [t('logs.accountId'), log.accountId || '–'],
    [t('logs.colTokens'), log.tokens ? log.tokens.toLocaleString(lang) : '–'],
    [t('logs.colCredits'), log.credits ? String(log.credits) : '–'],
    [t('logs.colDuration'), log.duration ? fmtMs(log.duration) : '–'],
  ]
  return (
    <div className="grid gap-4 rounded-lg border bg-card p-4 animate-rise lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        {fields.map(([k, v]) => (
          <Fragment key={k}>
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="min-w-0 break-all font-mono text-[12.5px]">{v}</dd>
          </Fragment>
        ))}
      </dl>
      {log.status === 'error' ? (
        <div className="min-w-0 space-y-2">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <ErrorTypeBadge type={log.errorType || 'unknown'} />
              {meta && <span className="text-xs text-muted-foreground">{t(meta.hint)}</span>}
            </div>
            <CopyButton value={log.error} label={t('common.copy')} />
          </div>
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-3 font-mono text-[12px] leading-relaxed text-destructive">
            {log.error || t('logs.noErrorText')}
          </pre>
        </div>
      ) : (
        <div className="flex items-center text-sm text-muted-foreground">{t('logs.successDetail')}</div>
      )}
    </div>
  )
}
