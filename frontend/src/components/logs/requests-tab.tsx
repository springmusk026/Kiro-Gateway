import { Filter, ScrollText, Search, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { EmptyState } from '@/components/app/empty-state'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, maskEmail, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useRequestPage, useUsageBy } from '@/lib/queries.history'
import { TablePagination } from '@/components/app/table-pagination'
import { useAccountLookup, useApiKeys } from '@/lib/queries.settings'
import type { RequestFilters, RequestRecord, StorageInfo } from '@/lib/types'
import { cn } from '@/lib/utils'
import { HistorySummary } from './history-summary'
import { HISTORY_ENDPOINTS, RANGES, exactTime, fmtMs, shortUA, unix, type RangeId } from './history-bits'
import { ENDPOINT_LABEL, ERROR_TYPES, EndpointChip, StatusBadge } from './log-meta'
import { RequestDetailSheet } from './request-detail'

const RANGE_KEY = 'kiro_logs_range'

function readRange(): RangeId {
  try {
    const v = localStorage.getItem(RANGE_KEY) as RangeId | null
    if (v && RANGES.some((r) => r.id === v)) return v
  } catch {
    /* storage unavailable */
  }
  return '24h'
}

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

/** Lookups shared by the table and the detail sheet. */
export function useRecordLabels(privacy: boolean) {
  const accounts = useAccountLookup()
  const keys = useApiKeys()
  const keyNames = useMemo(() => {
    const m: Record<string, string> = {}
    for (const k of keys.data ?? []) m[k.id] = k.name || k.keyMasked
    return m
  }, [keys.data])
  return {
    accounts: accounts.data ?? {},
    keys: keys.data ?? [],
    account: (r: Pick<RequestRecord, 'accountId' | 'accountEmail'>) => {
      const email = r.accountEmail || (r.accountId ? accounts.data?.[r.accountId] : '')
      if (email) return privacy ? maskEmail(email) : email
      return r.accountId ? r.accountId.slice(0, 8) : '–'
    },
    apiKey: (id: string | undefined) => (id ? keyNames[id] || id.slice(0, 8) : '–'),
  }
}

export function RequestsTab({ live, privacy, storage }: { live: boolean; privacy: boolean; storage: StorageInfo }) {
  const { t, lang } = useI18n()
  const labels = useRecordLabels(privacy)
  const [range, setRangeState] = useState<RangeId>(() => (typeof window === 'undefined' ? '24h' : readRange()))
  const [status, setStatus] = useState('all')
  const [endpoint, setEndpoint] = useState('all')
  const [model, setModel] = useState('all')
  const [account, setAccount] = useState('all')
  const [apiKey, setApiKey] = useState('all')
  const [errType, setErrType] = useState('all')
  const [search, setSearch] = useState('')
  const q = useDebounced(search, 300)
  const [selected, setSelected] = useState<RequestRecord | null>(null)
  const [, tick] = useState(0)

  const setRange = (v: RangeId) => {
    setRangeState(v)
    try {
      localStorage.setItem(RANGE_KEY, v)
    } catch {
      /* storage unavailable */
    }
  }

  const hours = RANGES.find((r) => r.id === range)!.hours
  // "All" for the usage endpoints means the whole retention window.
  const usageHours = hours || (storage.logRetentionDays || 30) * 24 + 24
  const filters: RequestFilters = { status, endpoint, model, account, apiKey, errorType: errType, q, hours }
  const filterKey = JSON.stringify(filters)
  const narrowed = status !== 'all' || endpoint !== 'all' || model !== 'all' || account !== 'all' || apiKey !== 'all' || errType !== 'all' || q.trim() !== ''

  // Page navigation keeps a stack of keyset cursors; filters reset it.
  const [pageSize, setPageSizeState] = useState(() => {
    try {
      const v = Number(localStorage.getItem('kiro_page_size:requests'))
      return [25, 50, 100].includes(v) ? v : 50
    } catch {
      return 50
    }
  })
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])
  const pageIndex = cursors.length - 1
  useEffect(() => setCursors([undefined]), [filterKey, pageSize])
  const list = useRequestPage(filters, cursors[pageIndex], pageSize, live ? 5_000 : false)
  const firstPage = useRequestPage(filters, undefined, pageSize)
  const total = firstPage.data?.total
  const totalCapped = !!firstPage.data?.totalCapped
  const setPageSize = (n: number) => {
    setPageSizeState(n)
    try {
      localStorage.setItem('kiro_page_size:requests', String(n))
    } catch {
      /* storage unavailable */
    }
  }
  const goToPage = (p: number) => {
    if (p <= 0) return
    if (p - 1 < pageIndex) setCursors((c) => c.slice(0, p))
    else if (p - 1 === pageIndex + 1 && list.data?.hasMore && list.data.records.length) {
      const last = list.data.records[list.data.records.length - 1].id
      setCursors((c) => [...c, last])
    }
  }
  const models = useUsageBy('model', usageHours, 50)

  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 15_000)
    return () => clearInterval(id)
  }, [])

  const rows = useMemo(() => list.data?.records ?? [], [list.data])

  // Highlight rows that arrived since the previous poll (per filter set).
  const known = useRef<{ key: string; ids: Set<string> } | null>(null)
  const [fresh, setFresh] = useState<Set<string>>(new Set())
  useEffect(() => {
    if (!list.data || list.isPlaceholderData) return
    const ids = new Set(rows.map((r) => r.id))
    const prev = known.current
    known.current = { key: filterKey, ids }
    if (!prev || prev.key !== filterKey) return
    const added = rows.filter((r) => !prev.ids.has(r.id)).map((r) => r.id)
    if (!added.length) return
    setFresh(new Set(added))
    const id = setTimeout(() => setFresh(new Set()), 2600)
    return () => clearTimeout(id)
  }, [rows, filterKey, list.data, list.isPlaceholderData])


  const resetFilters = () => {
    setStatus('all')
    setEndpoint('all')
    setModel('all')
    setAccount('all')
    setApiKey('all')
    setErrType('all')
    setSearch('')
  }

  const accountOptions = Object.entries(labels.accounts).sort((a, b) => a[1].localeCompare(b[1]))
  const modelOptions = (models.data ?? []).map((g) => g.key).filter(Boolean)
  if (model !== 'all' && !modelOptions.includes(model)) modelOptions.unshift(model)

  return (
    <div className="space-y-4">
      <HistorySummary rows={rows} narrowed={narrowed} usageHours={usageHours} loading={list.isLoading} hasMore={pageIndex > 0 || !!list.data?.hasMore} />

      <Card className="gap-0 overflow-hidden py-0 shadow-xs">
        <div className="space-y-2 border-b p-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative min-w-[220px] flex-1">
              <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="pl-9"
                placeholder={t('logs.h.searchPlaceholder')}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                aria-label={t('logs.h.searchPlaceholder')}
              />
            </div>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={range}
              onValueChange={(v) => v && setRange(v as RangeId)}
              aria-label={t('logs.h.range')}
            >
              {RANGES.map((r) => (
                <ToggleGroupItem key={r.id} value={r.id} className="px-3 text-xs">
                  {t(r.label)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger size="sm" className="w-[130px]" aria-label={t('logs.colStatus')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('logs.allStatuses')}</SelectItem>
                <SelectItem value="success">{t('logs.success')}</SelectItem>
                <SelectItem value="error">{t('logs.error')}</SelectItem>
              </SelectContent>
            </Select>
            <Select value={endpoint} onValueChange={setEndpoint}>
              <SelectTrigger size="sm" className="w-[140px]" aria-label={t('logs.colEndpoint')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('logs.allEndpoints')}</SelectItem>
                {HISTORY_ENDPOINTS.map((e) => (
                  <SelectItem key={e} value={e}>
                    {t(ENDPOINT_LABEL[e])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={model} onValueChange={setModel}>
              <SelectTrigger size="sm" className="w-[190px]" aria-label={t('logs.colModel')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('logs.h.allModels')}</SelectItem>
                {modelOptions.map((m) => (
                  <SelectItem key={m} value={m} className="font-mono text-xs">
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={account} onValueChange={setAccount}>
              <SelectTrigger size="sm" className="w-[190px]" aria-label={t('logs.colAccount')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('logs.h.allAccounts')}</SelectItem>
                {accountOptions.map(([id, email]) => (
                  <SelectItem key={id} value={id}>
                    {email ? (privacy ? maskEmail(email) : email) : id.slice(0, 8)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={apiKey} onValueChange={setApiKey}>
              <SelectTrigger size="sm" className="w-[160px]" aria-label={t('logs.h.colKey')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('logs.h.allKeys')}</SelectItem>
                {labels.keys.map((k) => (
                  <SelectItem key={k.id} value={k.id}>
                    {k.name || k.keyMasked}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={errType} onValueChange={setErrType}>
              <SelectTrigger size="sm" className="w-[160px]" aria-label={t('logs.colErrorType')}>
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
            {(narrowed || search) && (
              <Button variant="ghost" size="sm" onClick={resetFilters}>
                <X />
                {t('logs.resetFilters')}
              </Button>
            )}
          </div>
        </div>

        {list.isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : list.isError ? (
          <EmptyState
            icon={ScrollText}
            title={t('common.error')}
            description={(list.error as Error).message}
            action={
              <Button size="sm" variant="outline" onClick={() => list.refetch()}>
                {t('common.retry')}
              </Button>
            }
          />
        ) : rows.length === 0 ? (
          narrowed ? (
            <EmptyState
              icon={Filter}
              title={t('logs.noMatchTitle')}
              description={t('logs.h.noMatchDesc')}
              action={
                <Button size="sm" variant="outline" onClick={resetFilters}>
                  {t('logs.resetFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState icon={ScrollText} title={t('logs.h.emptyTitle')} description={t('logs.h.emptyDesc')} />
          )
        ) : (
          <>
            <Table className={cn('min-w-[960px] text-[13px] transition-opacity [&_td]:px-1.5 [&_th]:px-1.5', list.isPlaceholderData && 'opacity-60')}>
              <TableHeader>
                <TableRow className="bg-muted/30 text-xs hover:bg-muted/30">
                  <TableHead className="!pl-4">{t('logs.colTime')}</TableHead>
                  <TableHead>{t('logs.colStatus')}</TableHead>
                  <TableHead>
                    {t('logs.colEndpoint')} <span className="font-normal text-muted-foreground">/ {t('logs.h.colProtocol')}</span>
                  </TableHead>
                  <TableHead>{t('logs.colModel')}</TableHead>
                  <TableHead>{t('logs.colAccount')}</TableHead>
                  <TableHead>{t('logs.h.colKey')}</TableHead>
                  <TableHead>{t('logs.h.colClient')}</TableHead>
                  <TableHead className="text-right">{t('logs.h.colTokensInOut')}</TableHead>
                  <TableHead className="text-right">{t('logs.colCredits')}</TableHead>
                  <TableHead className="text-right">{t('logs.h.colLatency')}</TableHead>
                  <TableHead className="!pr-4 text-right">{t('logs.h.colAttempts')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((r) => (
                  <TableRow
                    key={r.id}
                    tabIndex={0}
                    onClick={() => setSelected(r)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setSelected(r)
                      }
                    }}
                    className={cn(
                      'h-11 cursor-pointer transition-colors duration-700 focus-visible:bg-muted/50 focus-visible:outline-none',
                      fresh.has(r.id) && 'bg-primary/[0.07]',
                      selected?.id === r.id && 'bg-muted/60',
                    )}
                  >
                    <TableCell className="!pl-4">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="tabular text-muted-foreground">{timeAgo(unix(r.time), lang)}</span>
                        </TooltipTrigger>
                        <TooltipContent>{exactTime(r.time, lang)}</TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell>
                      <StatusBadge log={r} />
                    </TableCell>
                    <TableCell>
                      <EndpointChip endpoint={r.endpoint} />
                      {(r.protocol || r.stream) && (
                        <span className="mt-0.5 flex items-center gap-1 text-[11px] leading-tight text-muted-foreground">
                          {r.protocol && <span className="font-mono">{r.protocol}</span>}
                          {r.stream && <span className="rounded bg-muted px-1 text-[10px] uppercase">{t('logs.h.streamShort')}</span>}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="max-w-[190px]">
                      <span className="block truncate font-mono text-[12.5px]">{r.model || '–'}</span>
                      {r.status === 'error' && (r.error || r.errorType) && (
                        <span className="block truncate text-xs text-destructive/80">
                          {r.errorType && <ErrorTypeLabel type={r.errorType} />}
                          {r.errorType && r.error && ' · '}
                          {r.error}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="max-w-[140px] truncate text-muted-foreground">{labels.account(r)}</TableCell>
                    <TableCell className="max-w-[110px] truncate text-muted-foreground">{labels.apiKey(r.apiKeyId)}</TableCell>
                    <TableCell className="max-w-[120px] truncate text-muted-foreground" title={r.userAgent}>
                      {shortUA(r.userAgent) || '–'}
                    </TableCell>
                    <TableCell className="text-right tabular">
                      {r.inputTokens || r.outputTokens ? (
                        <>
                          {compact(r.inputTokens)}
                          <span className="text-muted-foreground"> / </span>
                          {compact(r.outputTokens)}
                        </>
                      ) : (
                        '–'
                      )}
                    </TableCell>
                    <TableCell className="text-right tabular">{r.credits ? r.credits.toFixed(3) : '–'}</TableCell>
                    <TableCell className={cn('text-right tabular', r.durationMs >= 30_000 && 'text-warning')}>
                      <span className="block leading-tight">{r.durationMs ? fmtMs(r.durationMs) : '–'}</span>
                      {r.ttfbMs > 0 && (
                        <span className="block text-[11px] leading-tight text-muted-foreground">
                          {t('logs.h.ttfb')} {fmtMs(r.ttfbMs)}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className={cn('!pr-4 text-right tabular', r.attempts > 1 && 'font-medium text-warning')}>
                      {r.attempts || '–'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <TablePagination
              page={pageIndex + 1}
              pageCount={pageIndex + 1 + (list.data?.hasMore ? 1 : 0)}
              pageSize={pageSize}
              total={total}
              totalCapped={totalCapped}
              sequential
              shown={rows.length}
              hasNext={!!list.data?.hasMore}
              busy={list.isFetching && list.isPlaceholderData}
              onPage={goToPage}
              onPageSize={setPageSize}
              sizes={[25, 50, 100]}
            />
          </>
        )}
      </Card>

      <RequestDetailSheet
        record={selected}
        onClose={() => setSelected(null)}
        labels={labels}
        bodiesEnabled={storage.logBodiesKB > 0}
      />
    </div>
  )
}

function ErrorTypeLabel({ type }: { type: string }) {
  const { t } = useI18n()
  const meta = ERROR_TYPES.find((e) => e.id === type)
  return <span className="font-medium">{meta ? t(meta.label) : type}</span>
}
