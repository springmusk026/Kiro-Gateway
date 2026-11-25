import { ArrowLeft, Filter, MessagesSquare, Search, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { CopyButton } from '@/components/app/copy-button'
import { EmptyState } from '@/components/app/empty-state'
import { TablePagination } from '@/components/app/table-pagination'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { usePagination } from '@/hooks/use-pagination'
import { compact, timeAgo, usd } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useConversation, useConversationPage } from '@/lib/queries.history'
import type { ConversationSummary, RequestRecord, StorageInfo } from '@/lib/types'
import { cn } from '@/lib/utils'
import { RANGES, exactTime, fmtMs, shortUA, unix, type RangeId } from './history-bits'
import { StatusBadge } from './log-meta'
import { RequestDetailSheet } from './request-detail'
import { useRecordLabels } from './requests-tab'

const RANGE_KEY = 'kiro_conversations_range'
const PAGE_SIZE_KEY = 'kiro_page_size:conversations'

function readLocal(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}
function writeLocal(key: string, v: string) {
  try {
    localStorage.setItem(key, v)
  } catch {
    /* storage unavailable */
  }
}

/** "1h 4m", "12m 3s", "45s" */
function span(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return h < 48 ? `${h}h ${m % 60}m` : `${Math.floor(h / 24)}d ${h % 24}h`
}

/** Conversations: request-log rows grouped by the upstream conversation id. */
export function ConversationsTab({
  privacy,
  storage,
  conversationId,
  onOpen,
  onBack,
}: {
  privacy: boolean
  storage: StorageInfo
  conversationId?: string
  onOpen: (id: string) => void
  onBack: () => void
}) {
  if (conversationId) return <ConversationDetailView id={conversationId} privacy={privacy} storage={storage} onBack={onBack} />
  return <ConversationList privacy={privacy} onOpen={onOpen} />
}

function ConversationList({ privacy, onOpen }: { privacy: boolean; onOpen: (id: string) => void }) {
  const { t, lang } = useI18n()
  const labels = useRecordLabels(privacy)
  const [range, setRangeState] = useState<RangeId>(() => {
    const v = typeof window === 'undefined' ? null : (readLocal(RANGE_KEY) as RangeId | null)
    return v && RANGES.some((r) => r.id === v) ? v : '24h'
  })
  const [search, setSearch] = useState('')
  const [q, setQ] = useState('')
  useEffect(() => {
    const id = setTimeout(() => setQ(search), 300)
    return () => clearTimeout(id)
  }, [search])
  const [pageSize, setPageSizeState] = useState(() => {
    const v = Number(typeof window === 'undefined' ? 0 : readLocal(PAGE_SIZE_KEY))
    return [25, 50, 100].includes(v) ? v : 25
  })
  const hours = RANGES.find((r) => r.id === range)!.hours
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])
  const pageIndex = cursors.length - 1
  useEffect(() => setCursors([undefined]), [hours, q, pageSize])
  const list = useConversationPage(hours, q, cursors[pageIndex], pageSize)
  const rows = list.data?.conversations ?? []

  const setRange = (v: RangeId) => {
    setRangeState(v)
    writeLocal(RANGE_KEY, v)
  }
  const setPageSize = (n: number) => {
    setPageSizeState(n)
    writeLocal(PAGE_SIZE_KEY, String(n))
  }
  const goToPage = (p: number) => {
    if (p <= 0) return
    if (p - 1 < pageIndex) setCursors((c) => c.slice(0, p))
    else if (p - 1 === pageIndex + 1 && list.data?.hasMore && rows.length) setCursors((c) => [...c, rows[rows.length - 1].id])
  }

  return (
    <Card className="gap-0 overflow-hidden py-0 shadow-xs">
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <div className="relative min-w-[220px] flex-1">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9"
            placeholder={t('logs.c.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label={t('logs.c.searchPlaceholder')}
          />
        </div>
        <ToggleGroup type="single" variant="outline" size="sm" value={range} onValueChange={(v) => v && setRange(v as RangeId)} aria-label={t('logs.h.range')}>
          {RANGES.map((r) => (
            <ToggleGroupItem key={r.id} value={r.id} className="px-3 text-xs">
              {t(r.label)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {list.isLoading ? (
        <div className="space-y-2 p-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : list.isError ? (
        <EmptyState
          icon={MessagesSquare}
          title={t('common.error')}
          description={(list.error as Error).message}
          action={
            <Button size="sm" variant="outline" onClick={() => list.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      ) : rows.length === 0 ? (
        q.trim() ? (
          <EmptyState
            icon={Filter}
            title={t('logs.noMatchTitle')}
            description={t('logs.c.noMatchDesc')}
            action={
              <Button size="sm" variant="outline" onClick={() => setSearch('')}>
                <X />
                {t('logs.resetFilters')}
              </Button>
            }
          />
        ) : (
          <EmptyState icon={MessagesSquare} title={t('logs.c.emptyTitle')} description={t('logs.c.emptyDesc')} />
        )
      ) : (
        <>
          <Table className={cn('min-w-[900px] text-[13px] transition-opacity [&_td]:px-1.5 [&_th]:px-1.5', list.isPlaceholderData && 'opacity-60')}>
            <TableHeader>
              <TableRow className="bg-muted/30 text-xs hover:bg-muted/30">
                <TableHead className="!pl-4">{t('logs.c.colLast')}</TableHead>
                <TableHead>{t('logs.c.colConversation')}</TableHead>
                <TableHead className="text-right">{t('logs.c.colRequests')}</TableHead>
                <TableHead>{t('logs.colModel')}</TableHead>
                <TableHead>{t('logs.colAccount')}</TableHead>
                <TableHead>{t('logs.h.colKey')}</TableHead>
                <TableHead>{t('logs.h.colClient')}</TableHead>
                <TableHead className="text-right">{t('logs.h.colTokensInOut')}</TableHead>
                <TableHead className="text-right">{t('logs.c.colCacheRead')}</TableHead>
                <TableHead className="!pr-4 text-right">{t('logs.colCredits')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((c) => (
                <ConversationRow key={c.id} c={c} labels={labels} lang={lang} onOpen={() => onOpen(c.id)} />
              ))}
            </TableBody>
          </Table>
          <TablePagination
            page={pageIndex + 1}
            pageCount={pageIndex + 1 + (list.data?.hasMore ? 1 : 0)}
            pageSize={pageSize}
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
  )
}

function ConversationRow({
  c,
  labels,
  lang,
  onOpen,
}: {
  c: ConversationSummary
  labels: ReturnType<typeof useRecordLabels>
  lang: string
  onOpen: () => void
}) {
  const { t } = useI18n()
  const models = c.models ?? []
  const accounts = c.accounts ?? []
  const keys = c.apiKeys ?? []
  const more = (n: number) => (n > 1 ? <span className="text-muted-foreground"> +{n - 1}</span> : null)
  return (
    <TableRow
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onOpen()
        }
      }}
      className="h-11 cursor-pointer focus-visible:bg-muted/50 focus-visible:outline-none"
    >
      <TableCell className="!pl-4">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="tabular text-muted-foreground">{timeAgo(unix(c.lastSeen), lang)}</span>
          </TooltipTrigger>
          <TooltipContent>{exactTime(c.lastSeen, lang)}</TooltipContent>
        </Tooltip>
      </TableCell>
      <TableCell className="max-w-[200px]">
        <span className="block truncate font-mono text-[12.5px]" title={c.id}>
          {c.id}
        </span>
        <span className="block text-[11px] leading-tight text-muted-foreground">
          {t('logs.c.lasted', { d: span(Date.parse(c.lastSeen) - Date.parse(c.firstSeen)) })}
        </span>
      </TableCell>
      <TableCell className="text-right tabular">
        <span className="font-medium">{c.requests.toLocaleString(lang)}</span>
        {(c.errors > 0 || c.cancelled > 0) && (
          <span className="block text-[11px] leading-tight">
            {c.errors > 0 && <span className="text-destructive">{t('logs.c.errors', { n: c.errors })}</span>}
            {c.errors > 0 && c.cancelled > 0 && <span className="text-muted-foreground"> · </span>}
            {c.cancelled > 0 && <span className="text-muted-foreground">{t('logs.c.cancelled', { n: c.cancelled })}</span>}
          </span>
        )}
      </TableCell>
      <TableCell className="max-w-[190px] truncate font-mono text-[12.5px]" title={models.join(', ')}>
        {models[0] || '–'}
        {more(models.length)}
      </TableCell>
      <TableCell className="max-w-[150px] truncate text-muted-foreground">
        {accounts.length ? labels.account({ accountId: accounts[0] }) : '–'}
        {more(accounts.length)}
      </TableCell>
      <TableCell className="max-w-[110px] truncate text-muted-foreground">
        {labels.apiKey(keys[0])}
        {more(keys.length)}
      </TableCell>
      <TableCell className="max-w-[120px] truncate text-muted-foreground" title={c.userAgent}>
        {shortUA(c.userAgent) || '–'}
      </TableCell>
      <TableCell className="text-right tabular">
        {compact(c.inputTokens)}
        <span className="text-muted-foreground"> / </span>
        {compact(c.outputTokens)}
      </TableCell>
      <TableCell className="text-right tabular text-muted-foreground">{c.cacheReadTokens ? compact(c.cacheReadTokens) : '–'}</TableCell>
      <TableCell className="!pr-4 text-right tabular">
        <span className="block leading-tight">{c.credits ? c.credits.toFixed(3) : '–'}</span>
        {!!c.costUsd && (
          <span className="block text-[11px] leading-tight text-muted-foreground" title={t('pricing.costTitle')}>
            {usd(c.costUsd)}
          </span>
        )}
      </TableCell>
    </TableRow>
  )
}

function ConversationDetailView({ id, privacy, storage, onBack }: { id: string; privacy: boolean; storage: StorageInfo; onBack: () => void }) {
  const { t, lang } = useI18n()
  const labels = useRecordLabels(privacy)
  const conv = useConversation(id)
  const reqs = useMemo(() => conv.data?.requests ?? [], [conv.data])
  const [selected, setSelected] = useState<RequestRecord | null>(null)
  const pg = usePagination(reqs, { storageKey: 'conversation-timeline', pageSize: 25, resetKey: id })
  const start = reqs.length ? Date.parse(reqs[0].time) : 0
  const totals = useMemo(
    () =>
      reqs.reduce(
        (a, r) => ({
          input: a.input + r.inputTokens,
          output: a.output + r.outputTokens,
          cache: a.cache + (r.cacheReadTokens ?? 0),
          credits: a.credits + r.credits,
          cost: a.cost + (r.costUsd ?? 0),
          errors: a.errors + (r.status === 'error' ? 1 : 0),
        }),
        { input: 0, output: 0, cache: 0, credits: 0, cost: 0, errors: 0 },
      ),
    [reqs],
  )
  const last = reqs.length ? Date.parse(reqs[reqs.length - 1].time) : 0

  return (
    <Card className="gap-0 overflow-hidden py-0 shadow-xs">
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <Button variant="ghost" size="sm" onClick={onBack}>
          <ArrowLeft />
          {t('logs.c.back')}
        </Button>
        <div className="flex min-w-0 basis-full items-center gap-1 rounded-lg border bg-muted/40 py-1 pl-3 pr-1 sm:basis-0 sm:flex-1">
          <span className="shrink-0 text-xs text-muted-foreground">{t('logs.c.id')}</span>
          <code className="min-w-0 flex-1 truncate font-mono text-[12.5px]">{id}</code>
          <CopyButton value={id} />
        </div>
      </div>
      {conv.isLoading ? (
        <div className="space-y-2 p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : conv.isError ? (
        <EmptyState
          icon={MessagesSquare}
          title={t('logs.c.notFound')}
          description={(conv.error as Error).message}
          action={
            <Button size="sm" variant="outline" onClick={onBack}>
              {t('logs.c.back')}
            </Button>
          }
        />
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-px border-b bg-border sm:grid-cols-4 lg:grid-cols-7 [&>div]:bg-card [&>div]:px-4 [&>div]:py-3">
            <Stat label={t('logs.c.colRequests')} value={reqs.length.toLocaleString(lang)} />
            <Stat label={t('logs.c.duration')} value={span(last - start)} />
            <Stat label={t('logs.h.inputTokens')} value={compact(totals.input)} />
            <Stat label={t('logs.h.outputTokens')} value={compact(totals.output)} />
            <Stat label={t('logs.c.colCacheRead')} value={compact(totals.cache)} />
            <Stat label={t('logs.colCredits')} value={totals.credits.toFixed(3)} />
            <Stat label={t('pricing.listPrice')} value={totals.cost ? usd(totals.cost) : '–'} title={t('pricing.costTitle')} className="col-span-2 lg:col-span-1" />
          </dl>
          {conv.data?.truncated && <p className="border-b px-4 py-2 text-xs text-muted-foreground">{t('logs.c.truncated', { n: reqs.length })}</p>}
          <ol className="divide-y">
            {pg.pageItems.map((r, i) => {
              const n = (pg.page - 1) * pg.pageSize + i + 1
              return (
                <li key={r.id}>
                  <button
                    type="button"
                    onClick={() => setSelected(r)}
                    className={cn(
                      'grid w-full grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-2.5 text-left text-[13px] transition-colors hover:bg-muted/40 focus-visible:bg-muted/50 focus-visible:outline-none',
                      selected?.id === r.id && 'bg-muted/60',
                    )}
                  >
                    <span className="grid size-7 place-items-center rounded-full border bg-card text-xs font-medium tabular text-muted-foreground">{n}</span>
                    <span className="min-w-0">
                      <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                        <StatusBadge log={r} />
                        <span className="truncate font-mono text-[12.5px]">{r.model || '–'}</span>
                        {r.attempts > 1 && (
                          <Badge variant="outline" className="font-normal text-warning">
                            {t('logs.c.attempts', { n: r.attempts })}
                          </Badge>
                        )}
                      </span>
                      <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                        <span title={exactTime(r.time, lang)}>{exactTime(r.time, lang)}</span>
                        {n > 1 && <span> · +{span(Date.parse(r.time) - start)}</span>}
                        {r.status === 'error' && r.error && <span className="text-destructive/80"> · {r.error}</span>}
                      </span>
                    </span>
                    <span className="text-right text-xs tabular">
                      <span className="block">
                        {compact(r.inputTokens)}
                        <span className="text-muted-foreground"> / </span>
                        {compact(r.outputTokens)}
                      </span>
                      <span className="block text-muted-foreground">{r.durationMs ? fmtMs(r.durationMs) : '–'}</span>
                    </span>
                  </button>
                </li>
              )
            })}
          </ol>
          <TablePagination page={pg.page} pageCount={pg.pageCount} pageSize={pg.pageSize} total={pg.total} onPage={pg.setPage} onPageSize={pg.setPageSize} />
        </>
      )}
      <RequestDetailSheet
        record={selected}
        onClose={() => setSelected(null)}
        labels={labels}
        bodiesEnabled={storage.logBodiesKB > 0}
        currentConversation={id}
      />
    </Card>
  )
}

function Stat({ label, value, title, className }: { label: string; value: string; title?: string; className?: string }) {
  return (
    <div className={cn('min-w-0', className)} title={title}>
      <dt className="truncate text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-base font-semibold tabular">{value}</dd>
    </div>
  )
}
