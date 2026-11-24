import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { compact } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useUsageSeries } from '@/lib/queries.history'
import type { RequestRecord } from '@/lib/types'
import { cn } from '@/lib/utils'
import { fmtMs } from './history-bits'

function Metric({
  label,
  value,
  format,
  tone,
  sub,
}: {
  label: string
  value: number
  format: (n: number) => string
  tone?: string
  sub?: string
}) {
  const n = useAnimatedNumber(value)
  return (
    <div className="min-w-0 px-4 py-3 sm:px-5">
      <p className="truncate text-xs font-medium text-muted-foreground">{label}</p>
      <p className={cn('mt-1 text-xl font-semibold tracking-tight tabular', tone)}>{format(n)}</p>
      {sub && <p className="truncate text-[11px] text-muted-foreground tabular">{sub}</p>}
    </div>
  )
}

type Totals = { requests: number; errors: number; input: number; output: number; credits: number; latency: number }

/**
 * Totals for the selected range from /usage/series, or — when filters narrow the
 * list — computed from the rows loaded so far.
 */
export function HistorySummary({
  rows,
  narrowed,
  usageHours,
  loading,
  hasMore,
}: {
  rows: RequestRecord[]
  narrowed: boolean
  usageHours: number
  loading: boolean
  hasMore: boolean
}) {
  const { t } = useI18n()
  const bucket = usageHours <= 48 ? 3600 : 86400
  const series = useUsageSeries(usageHours, bucket, { enabled: !narrowed, refetchInterval: 15_000 })

  let totals: Totals
  if (!narrowed) {
    const pts = series.data?.points ?? []
    let latW = 0
    totals = { requests: 0, errors: 0, input: 0, output: 0, credits: 0, latency: 0 }
    for (const p of pts) {
      totals.requests += p.requests
      totals.errors += p.errors
      totals.input += p.inputTokens
      totals.output += p.outputTokens
      totals.credits += p.credits
      // avgLatencyMs covers successful requests only.
      const ok = p.requests - p.errors
      totals.latency += p.avgLatencyMs * ok
      latW += p.avgLatencyMs > 0 ? ok : 0
    }
    totals.latency = latW ? totals.latency / latW : 0
  } else {
    const timed = rows.filter((r) => r.status === 'success' && r.durationMs > 0)
    totals = {
      requests: rows.length,
      errors: rows.filter((r) => r.status !== 'success').length,
      input: rows.reduce((s, r) => s + r.inputTokens, 0),
      output: rows.reduce((s, r) => s + r.outputTokens, 0),
      credits: rows.reduce((s, r) => s + r.credits, 0),
      latency: timed.length ? timed.reduce((s, r) => s + r.durationMs, 0) / timed.length : 0,
    }
  }

  if (loading || (!narrowed && series.isLoading)) return <Skeleton className="h-[86px] w-full rounded-xl" />
  const n = totals.requests
  const rate = n ? (100 * (n - totals.errors)) / n : 0
  return (
    <Card className="grid grid-cols-2 gap-0 divide-border py-0 shadow-xs sm:grid-cols-3 lg:grid-cols-5 lg:divide-x">
      <Metric
        label={narrowed ? t('logs.h.sumLoaded') : t('logs.sumRequests')}
        value={n}
        format={(v) => Math.round(v).toLocaleString() + (narrowed && hasMore ? '+' : '')}
        sub={narrowed ? t('logs.h.sumLoadedHint') : t('logs.h.sumRangeHint')}
      />
      <Metric
        label={t('logs.sumSuccess')}
        value={rate}
        format={(v) => (n ? v.toFixed(1) + '%' : '–')}
        tone={!n ? undefined : rate >= 95 ? 'text-success' : rate >= 80 ? 'text-warning' : 'text-destructive'}
        sub={n ? t('logs.h.sumErrors', { n: totals.errors.toLocaleString() }) : undefined}
      />
      <Metric label={t('logs.sumLatency')} value={totals.latency} format={(v) => (totals.latency ? fmtMs(v) : '–')} />
      <Metric
        label={t('logs.sumTokens')}
        value={totals.input + totals.output}
        format={(v) => compact(Math.round(v))}
        sub={t('logs.h.sumInOut', { in: compact(totals.input), out: compact(totals.output) })}
      />
      <Metric label={t('logs.sumCredits')} value={totals.credits} format={(v) => v.toFixed(2)} />
    </Card>
  )
}
