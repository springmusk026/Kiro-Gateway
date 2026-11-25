import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'
import type { ReactNode } from 'react'
import { fmtMs } from '@/components/logs/history-bits'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { compact } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsSummary } from '@/lib/types'
import { cn } from '@/lib/utils'
import { change, ratio, type AnalyticsRange } from './lib'

/** Which direction is an improvement: drives the delta colour. */
type Good = 'up' | 'down' | 'neutral'

/**
 * Change vs the previous window. `points` compares two ratios (0..1) in percentage points;
 * otherwise the relative change in percent. Arrow + sign carry the direction, colour only the judgement.
 */
export function Delta({ cur, prev, good, points, className }: { cur: number; prev: number; good: Good; points?: boolean; className?: string }) {
  const { t } = useI18n()
  let v: number | null
  let text: string
  if (points) {
    v = cur - prev
    text = `${Math.abs(v * 100).toFixed(1)} pp`
    if (!prev && !cur) v = null
  } else {
    v = change(cur, prev)
    if (v === null) {
      if (cur > 0 && prev === 0) {
        return <span className={cn('text-xs font-medium text-muted-foreground', className)}>{t('analytics.delta.new')}</span>
      }
      return null
    }
    const pct = Math.abs(v * 100)
    text = `${pct >= 100 ? Math.round(pct).toLocaleString() : pct.toFixed(1)}%`
  }
  if (v === null) return null
  const flat = points ? Math.abs(v) < 0.0005 : Math.abs(v) < 0.005
  const up = v > 0
  const tone = flat || good === 'neutral' ? 'muted' : (up && good === 'up') || (!up && good === 'down') ? 'good' : 'bad'
  const Icon = flat ? Minus : up ? ArrowUpRight : ArrowDownRight
  return (
    <span
      className={cn(
        'inline-flex items-center gap-0.5 text-xs font-medium tabular',
        tone === 'good' && 'text-success',
        tone === 'bad' && 'text-destructive',
        tone === 'muted' && 'text-muted-foreground',
        className,
      )}
    >
      <Icon className="size-3.5" aria-hidden />
      {flat ? t('analytics.delta.flat') : (up ? '+' : '−') + text}
    </span>
  )
}

function Kpi({
  label,
  value,
  format,
  delta,
  hint,
  children,
}: {
  label: string
  value: number
  format: (n: number) => ReactNode
  delta?: ReactNode
  hint?: ReactNode
  children?: ReactNode
}) {
  const n = useAnimatedNumber(value)
  return (
    <Card className="min-w-0 gap-0 p-4 transition-shadow hover:shadow-md sm:p-5">
      <p className="truncate text-[13px] font-medium text-muted-foreground">{label}</p>
      <div className="mt-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <p className="text-2xl font-semibold tracking-tight sm:text-[28px]">{format(n)}</p>
        {delta}
      </div>
      {hint && <div className="mt-1.5 truncate text-xs text-muted-foreground tabular">{hint}</div>}
      {children}
    </Card>
  )
}

const fmtBytes = (n: number) => {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${i ? n.toFixed(1) : n} ${units[i]}`
}

const pct = (r: number) => (r * 100).toFixed(1) + '%'

export function KpiGrid({ s, p, hours, range }: { s: AnalyticsSummary; p: AnalyticsSummary; hours: number; range: AnalyticsRange }) {
  const { t } = useI18n()
  // Requests the client abandoned are neither successes nor failures.
  const success = ratio(s.requests - s.errors - (s.cancelled ?? 0), s.requests - (s.cancelled ?? 0))
  const prevSuccess = ratio(p.requests - p.errors - (p.cancelled ?? 0), p.requests - (p.cancelled ?? 0))
  const cache = Math.min(1, ratio(s.cacheReadTokens, s.inputTokens))
  const prevCache = Math.min(1, ratio(p.cacheReadTokens, p.inputTokens))
  const retried = ratio(s.retried, s.requests)
  const prevRetried = ratio(p.retried, p.requests)
  const vs = t(`analytics.prev.${range}` as const)
  return (
    <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4 stagger" title={vs}>
      <Kpi
        label={t('analytics.kpi.requests')}
        value={s.requests}
        format={(n) => (n >= 100_000 ? compact(Math.round(n)) : Math.round(n).toLocaleString())}
        delta={<Delta cur={s.requests} prev={p.requests} good="neutral" />}
        hint={t('analytics.kpi.requestsHint', { n: compact(Math.round((s.requests / hours) * 10) / 10) })}
      />
      <Kpi
        label={t('analytics.kpi.successRate')}
        value={success * 100}
        format={(n) => (s.requests ? n.toFixed(1) + '%' : '—')}
        delta={s.requests > 0 && p.requests > 0 ? <Delta cur={success} prev={prevSuccess} good="up" points /> : undefined}
        hint={t('analytics.kpi.successHint', { n: s.errors.toLocaleString() })}
      >
        {s.requests > 0 && (
          <div className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-destructive/20" aria-hidden>
            <div className="h-full rounded-full bg-success transition-[width] duration-700" style={{ width: `${success * 100}%` }} />
          </div>
        )}
      </Kpi>
      <Kpi
        label={t('analytics.kpi.credits')}
        value={s.credits}
        format={(n) => (n >= 10_000 ? compact(Math.round(n)) : n.toFixed(n >= 100 ? 0 : 2))}
        delta={<Delta cur={s.credits} prev={p.credits} good="neutral" />}
        hint={t('analytics.kpi.creditsHint', { n: s.requests ? (s.credits / s.requests).toFixed(3) : '–' })}
      />
      <Kpi
        label={t('analytics.kpi.tokens')}
        value={s.inputTokens}
        format={(n) => (
          <>
            {compact(Math.round(n))}
            <span className="text-base font-medium text-muted-foreground sm:text-lg"> / {compact(s.outputTokens)}</span>
          </>
        )}
        delta={<Delta cur={s.inputTokens + s.outputTokens} prev={p.inputTokens + p.outputTokens} good="neutral" />}
        hint={t('analytics.kpi.tokensHint', { n: compact(s.inputTokens + s.outputTokens) })}
      />
      <Kpi
        label={t('analytics.kpi.cache')}
        value={cache * 100}
        format={(n) => (s.inputTokens ? n.toFixed(1) + '%' : '—')}
        delta={s.inputTokens > 0 && p.inputTokens > 0 ? <Delta cur={cache} prev={prevCache} good="up" points /> : undefined}
        hint={t('analytics.kpi.cacheHint', { r: compact(s.cacheReadTokens), w: compact(s.cacheWriteTokens) })}
      />
      <Kpi
        label={t('analytics.kpi.latency')}
        value={s.p50LatencyMs}
        format={(n) => (s.p50LatencyMs ? fmtMs(n) : '—')}
        delta={<Delta cur={s.p50LatencyMs} prev={p.p50LatencyMs} good="down" />}
        hint={t('analytics.kpi.latencyHint', { p95: s.p95LatencyMs ? fmtMs(s.p95LatencyMs) : '–', p99: s.p99LatencyMs ? fmtMs(s.p99LatencyMs) : '–' })}
      />
      <Kpi
        label={t('analytics.kpi.ttft')}
        value={s.p50TtfbMs}
        format={(n) => (s.p50TtfbMs ? fmtMs(n) : '—')}
        delta={<Delta cur={s.p50TtfbMs} prev={p.p50TtfbMs} good="down" />}
        hint={t('analytics.kpi.ttftHint', { v: s.p95TtfbMs ? fmtMs(s.p95TtfbMs) : '–' })}
      />
      <Kpi
        label={t('analytics.kpi.retried')}
        value={retried * 100}
        format={(n) => (s.requests ? n.toFixed(1) + '%' : '—')}
        delta={s.requests > 0 && p.requests > 0 ? <Delta cur={retried} prev={prevRetried} good="down" points /> : undefined}
        hint={t('analytics.kpi.retriedHint', { n: s.retried.toLocaleString(), a: s.avgAttempts ? s.avgAttempts.toFixed(2) : '–' })}
      />
    </div>
  )
}

function Mini({ label, value, delta, title }: { label: string; value: ReactNode; delta?: ReactNode; title?: string }) {
  return (
    <div className="min-w-0 px-4 py-3" title={title}>
      <p className="truncate text-xs text-muted-foreground">{label}</p>
      <div className="mt-0.5 flex flex-wrap items-baseline gap-x-1.5">
        <span className="text-base font-semibold tabular">{value}</span>
        {delta}
      </div>
    </div>
  )
}

export function SecondaryStats({ s, p }: { s: AnalyticsSummary; p: AnalyticsSummary }) {
  const { t } = useI18n()
  return (
    <Card className="grid grid-cols-2 gap-0 py-1 sm:grid-cols-4 xl:grid-cols-8">
      <Mini label={t('analytics.mini.throttled')} value={s.throttled.toLocaleString()} delta={<Delta cur={s.throttled} prev={p.throttled} good="down" />} />
      <Mini label={t('analytics.mini.clientErrors')} value={s.clientErrors.toLocaleString()} delta={<Delta cur={s.clientErrors} prev={p.clientErrors} good="down" />} />
      <Mini
        label={t('analytics.mini.streamed')}
        value={s.requests ? pct(ratio(s.streamed, s.requests)) : '–'}
        title={s.streamed.toLocaleString()}
      />
      <Mini label={t('analytics.mini.accounts')} value={s.accounts.toLocaleString()} delta={<Delta cur={s.accounts} prev={p.accounts} good="neutral" />} />
      <Mini label={t('analytics.mini.keys')} value={s.apiKeys.toLocaleString()} delta={<Delta cur={s.apiKeys} prev={p.apiKeys} good="neutral" />} />
      <Mini label={t('analytics.mini.models')} value={s.models.toLocaleString()} delta={<Delta cur={s.models} prev={p.models} good="neutral" />} />
      <Mini
        label={t('analytics.mini.tps')}
        value={
          s.outputTokensPerSec ? (
            <>
              {s.outputTokensPerSec.toFixed(0)} <span className="text-xs font-normal text-muted-foreground">{t('analytics.mini.tpsUnit')}</span>
            </>
          ) : (
            '–'
          )
        }
        delta={<Delta cur={s.outputTokensPerSec} prev={p.outputTokensPerSec} good="up" />}
      />
      <Mini
        label={t('analytics.mini.transferred')}
        value={fmtBytes(s.requestBytes + s.responseBytes)}
        title={t('analytics.mini.transferredHint', { up: fmtBytes(s.requestBytes), down: fmtBytes(s.responseBytes) })}
      />
    </Card>
  )
}

export function KpiSkeleton() {
  return (
    <>
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-[112px] rounded-xl" />
        ))}
      </div>
      <Skeleton className="h-[128px] rounded-xl" />
      <Skeleton className="h-[66px] rounded-xl" />
    </>
  )
}
