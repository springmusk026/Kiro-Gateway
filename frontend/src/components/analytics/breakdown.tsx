import { ArrowDown, ArrowUp, Boxes, CircleAlert, CircleCheck, Download, Filter, TriangleAlert } from 'lucide-react'
import { useMemo, useState } from 'react'
import { TablePagination } from '@/components/app/table-pagination'
import { usePagination } from '@/hooks/use-pagination'
import { EmptyState } from '@/components/app/empty-state'
import { fmtMs, unix } from '@/components/logs/history-bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { downloadCsv } from '@/lib/csv'
import { compact, timeAgo, usd } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { pricesLoaded } from '@/lib/pricing'
import type { AnalyticsDimension, AnalyticsGroup, AnalyticsReport } from '@/lib/types'
import type { MessageKey } from '@/locales/en'
import { cn } from '@/lib/utils'
import { FILTER_DIMS, useDimLabels, type FilterDim } from './lib'

const DIMS: { id: AnalyticsDimension; label: MessageKey }[] = [
  { id: 'model', label: 'analytics.dim.model' },
  { id: 'account', label: 'analytics.dim.account' },
  { id: 'apiKey', label: 'analytics.dim.apiKey' },
  { id: 'client', label: 'analytics.dim.client' },
  { id: 'endpoint', label: 'analytics.dim.endpoint' },
  { id: 'protocol', label: 'analytics.dim.protocol' },
  { id: 'status', label: 'analytics.dim.status' },
]

type SortKey =
  | 'name'
  | 'requests'
  | 'errorRate'
  | 'throttled'
  | 'inputTokens'
  | 'outputTokens'
  | 'cacheReadTokens'
  | 'credits'
  | 'costUsd'
  | 'p50LatencyMs'
  | 'p95LatencyMs'
  | 'p50TtfbMs'
  | 'avgAttempts'
  | 'lastSeen'

/** Columns; `cls` hides low-priority ones on small screens (the table scrolls inside its card otherwise). */
const COLS: { id: SortKey; label: MessageKey; cls?: string }[] = [
  { id: 'requests', label: 'analytics.col.requests' },
  { id: 'errorRate', label: 'analytics.col.errorRate' },
  { id: 'throttled', label: 'analytics.col.throttled', cls: 'hidden 2xl:table-cell' },
  { id: 'inputTokens', label: 'analytics.col.tokensIn', cls: 'hidden sm:table-cell' },
  { id: 'outputTokens', label: 'analytics.col.tokensOut', cls: 'hidden sm:table-cell' },
  { id: 'cacheReadTokens', label: 'analytics.col.cacheRead', cls: 'hidden xl:table-cell' },
  { id: 'credits', label: 'analytics.col.credits' },
  { id: 'costUsd', label: 'pricing.col', cls: 'hidden sm:table-cell' },
  { id: 'p50LatencyMs', label: 'analytics.col.p50', cls: 'hidden md:table-cell' },
  { id: 'p95LatencyMs', label: 'analytics.col.p95', cls: 'hidden lg:table-cell' },
  { id: 'p50TtfbMs', label: 'analytics.col.ttfb', cls: 'hidden lg:table-cell' },
  { id: 'avgAttempts', label: 'analytics.col.attempts', cls: 'hidden 2xl:table-cell' },
  { id: 'lastSeen', label: 'analytics.col.lastSeen', cls: 'hidden md:table-cell' },
]

const colCls = (id: SortKey) => COLS.find((c) => c.id === id)?.cls

function value(g: AnalyticsGroup, k: SortKey): number {
  switch (k) {
    case 'errorRate':
      return g.requests ? g.errors / g.requests : 0
    case 'lastSeen':
      return Date.parse(g.lastSeen) || 0
    case 'name':
      return 0
    default:
      return g[k]
  }
}

function StatusCode({ code }: { code: string }) {
  const n = Number(code)
  const ok = n >= 200 && n < 300
  const warn = n === 429 || (n >= 400 && n < 500)
  const Icon = ok ? CircleCheck : warn ? TriangleAlert : CircleAlert
  return (
    <Badge
      variant="outline"
      className={cn(
        'font-mono',
        ok && 'border-success/30 text-success',
        !ok && warn && 'border-warning/40 text-warning',
        !ok && !warn && 'border-destructive/30 text-destructive',
      )}
    >
      <Icon />
      {code || '–'}
    </Badge>
  )
}

export function BreakdownCard({ report, onFilter }: { report: AnalyticsReport; onFilter: (dim: FilterDim, key: string) => void }) {
  const { t, lang } = useI18n()
  const label = useDimLabels(t)
  const [dim, setDim] = useState<AnalyticsDimension>('model')
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'requests', desc: true })
  const groups = report.breakdowns?.[dim] ?? []
  const total = groups.reduce((s, g) => s + g.requests, 0)
  const max = groups.reduce((m, g) => Math.max(m, g.requests), 0)
  const filterable = (FILTER_DIMS as readonly string[]).includes(dim)
  const priced = pricesLoaded(report.pricing)

  const rows = useMemo(() => {
    const out = [...groups]
    out.sort((a, b) => {
      const d =
        sort.key === 'name'
          ? label(dim, a.key, a.label).text.localeCompare(label(dim, b.key, b.label).text)
          : value(a, sort.key) - value(b, sort.key)
      return sort.desc ? -d : d
    })
    return out
  }, [groups, sort, dim, label])
  const pg = usePagination(rows, { storageKey: 'analytics-breakdown', pageSize: 10, resetKey: `${dim}|${sort.key}|${sort.desc}` })

  // CSV of the current dimension, as sorted, for the loaded range and filters (all rows, not just this page).
  const exportCsv = () => {
    const day = (iso: string) => iso.slice(0, 16).replace(/[-:]/g, '').replace('T', '-')
    downloadCsv(
      `kiro-${dim}-${day(report.from)}_${day(report.to)}.csv`,
      ['name', 'key', 'requests', 'errors', 'error_rate', 'throttled', 'input_tokens', 'output_tokens', 'cache_read_tokens',
        'cache_write_tokens', 'credits', 'cost_usd', 'p50_latency_ms', 'p95_latency_ms', 'p50_ttfb_ms', 'avg_attempts', 'last_seen'],
      rows.map((g) => [
        label(dim, g.key, g.label).text,
        g.key,
        g.requests,
        g.errors,
        g.requests ? +(g.errors / g.requests).toFixed(4) : 0,
        g.throttled,
        g.inputTokens,
        g.outputTokens,
        g.cacheReadTokens,
        g.cacheWriteTokens,
        +g.credits.toFixed(4),
        +(g.costUsd ?? 0).toFixed(6),
        Math.round(g.p50LatencyMs),
        Math.round(g.p95LatencyMs),
        Math.round(g.p50TtfbMs),
        +g.avgAttempts.toFixed(2),
        g.lastSeen,
      ]),
    )
  }

  const toggleSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: key !== 'name' }))

  const head = (id: SortKey, text: string, cls?: string, align: 'left' | 'right' = 'right') => {
    const active = sort.key === id
    const Arrow = sort.desc ? ArrowDown : ArrowUp
    return (
      <TableHead key={id} className={cn(align === 'right' && 'text-right', cls)} aria-sort={active ? (sort.desc ? 'descending' : 'ascending') : undefined}>
        <button
          type="button"
          onClick={() => toggleSort(id)}
          className={cn(
            'inline-flex items-center gap-1 rounded-sm text-xs font-medium hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            active ? 'text-foreground' : 'text-muted-foreground',
          )}
          aria-label={t('analytics.sortBy', { col: text })}
        >
          {text}
          <Arrow className={cn('size-3', !active && 'invisible')} aria-hidden />
        </button>
      </TableHead>
    )
  }

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('analytics.breakdown')}</CardTitle>
        <CardDescription>{t('analytics.breakdownHint')}</CardDescription>
        <CardAction>
          <Button variant="outline" size="sm" onClick={exportCsv} disabled={!rows.length} aria-label={t('analytics.csvHint')} title={t('analytics.csvHint')}>
            <Download />
            CSV
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="-mx-1 overflow-x-auto px-1 pb-1">
          <Tabs value={dim} onValueChange={(v) => setDim(v as AnalyticsDimension)}>
            <TabsList>
              {DIMS.map((d) => (
                <TabsTrigger key={d.id} value={d.id} className="px-3 text-xs">
                  {t(d.label)}
                  <span className="ml-1 text-muted-foreground tabular">{report.breakdowns?.[d.id]?.length ?? 0}</span>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>
        {rows.length === 0 ? (
          <EmptyState icon={Boxes} title={t('analytics.noRows')} />
        ) : (
          <div className="-mx-6 border-t">
            <Table className="tabular">
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  {head('name', t('analytics.col.name'), 'pl-6 min-w-[150px]', 'left')}
                  {COLS.map((c) => head(c.id, t(c.label), cn(c.cls, c.id === 'lastSeen' && 'pr-6')))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {pg.pageItems.map((g) => {
                  const l = label(dim, g.key, g.label)
                  const canFilter = filterable && !!g.key
                  const errRate = g.requests ? g.errors / g.requests : 0
                  const apply = () => canFilter && onFilter(dim as FilterDim, g.key)
                  return (
                    <TableRow
                      key={g.key || '∅'}
                      className={cn('h-11', canFilter && 'cursor-pointer')}
                      onClick={apply}
                      onKeyDown={(e) => {
                        if (canFilter && (e.key === 'Enter' || e.key === ' ')) {
                          e.preventDefault()
                          apply()
                        }
                      }}
                      tabIndex={canFilter ? 0 : undefined}
                      title={canFilter ? t('analytics.filterBy', { name: l.text }) : l.title}
                    >
                      <TableCell className="max-w-[200px] pl-6 sm:max-w-[280px]">
                        <div className="flex min-w-0 items-center gap-2">
                          {dim === 'status' ? (
                            <StatusCode code={g.key} />
                          ) : (
                            <span
                              className={cn(
                                'truncate',
                                (dim === 'model' || dim === 'apiKey' || dim === 'protocol') && 'font-mono text-[13px]',
                                l.muted && 'text-muted-foreground italic',
                              )}
                            >
                              {l.text}
                            </span>
                          )}
                          {canFilter && <Filter className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity [tr:hover_&]:opacity-100" aria-hidden />}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-2">
                          <span className="hidden h-1.5 w-16 overflow-hidden rounded-full bg-muted sm:block" aria-hidden>
                            <span className="block h-full rounded-full bg-chart-1" style={{ width: `${max ? (100 * g.requests) / max : 0}%` }} />
                          </span>
                          <span className="font-medium" title={total ? `${((100 * g.requests) / total).toFixed(1)}%` : undefined}>
                            {g.requests.toLocaleString()}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className={cn('text-right', errRate >= 0.1 ? 'text-destructive' : errRate > 0 ? '' : 'text-muted-foreground')}>
                        {g.requests ? (errRate * 100).toFixed(1) + '%' : '–'}
                      </TableCell>
                      <TableCell className={cn('text-right', colCls('throttled'), !g.throttled && 'text-muted-foreground')}>{g.throttled.toLocaleString()}</TableCell>
                      <TableCell className={cn('text-right', colCls('inputTokens'))}>{compact(g.inputTokens)}</TableCell>
                      <TableCell className={cn('text-right', colCls('outputTokens'))}>{compact(g.outputTokens)}</TableCell>
                      <TableCell className={cn('text-right', colCls('cacheReadTokens'))}>{compact(g.cacheReadTokens)}</TableCell>
                      <TableCell className="text-right">{g.credits.toFixed(g.credits >= 1000 ? 0 : 2)}</TableCell>
                      <TableCell className={cn('text-right', colCls('costUsd'), !(priced && g.costUsd) && 'text-muted-foreground')}>
                        {priced ? usd(g.costUsd ?? 0) : '—'}
                      </TableCell>
                      <TableCell className={cn('text-right', colCls('p50LatencyMs'))}>{g.p50LatencyMs ? fmtMs(g.p50LatencyMs) : '–'}</TableCell>
                      <TableCell className={cn('text-right', colCls('p95LatencyMs'))}>{g.p95LatencyMs ? fmtMs(g.p95LatencyMs) : '–'}</TableCell>
                      <TableCell className={cn('text-right', colCls('p50TtfbMs'))}>{g.p50TtfbMs ? fmtMs(g.p50TtfbMs) : '–'}</TableCell>
                      <TableCell className={cn('text-right', colCls('avgAttempts'))}>{g.avgAttempts ? g.avgAttempts.toFixed(2) : '–'}</TableCell>
                      <TableCell className={cn('pr-6 text-right text-muted-foreground', colCls('lastSeen'))} title={g.lastSeen ? new Date(g.lastSeen).toLocaleString(lang) : undefined}>
                        {timeAgo(unix(g.lastSeen), lang)}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            <TablePagination page={pg.page} pageCount={pg.pageCount} pageSize={pg.pageSize} total={pg.total} onPage={pg.setPage} onPageSize={pg.setPageSize} />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
