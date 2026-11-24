import { Activity } from 'lucide-react'
import { useMemo } from 'react'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis, type TooltipContentProps } from 'recharts'
import { EmptyState } from '@/components/app/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { fmtMs } from '@/components/logs/history-bits'
import { compact } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useUsageSeries } from '@/lib/queries.history'

export type OverviewRange = '24h' | '7d' | '30d'
export const OVERVIEW_RANGES: Record<OverviewRange, { hours: number; bucket: number }> = {
  '24h': { hours: 24, bucket: 3600 },
  '7d': { hours: 168, bucket: 6 * 3600 },
  '30d': { hours: 720, bucket: 86400 },
}

type Row = {
  t: number
  label: string
  ok: number
  err: number
  input: number
  output: number
  credits: number
  latency: number
}

function ChartTooltip({ active, payload }: Partial<TooltipContentProps<number, string>>) {
  const { t } = useI18n()
  const r = payload?.[0]?.payload as Row | undefined
  if (!active || !r) return null
  const total = r.ok + r.err
  return (
    <div className="min-w-[180px] rounded-lg border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="mb-1.5 font-medium text-muted-foreground">{r.label}</p>
      <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5 tabular">
        <dt className="flex items-center gap-1.5">
          <span className="size-2 rounded-sm bg-chart-1" />
          {t('overview.success')}
        </dt>
        <dd className="text-right font-medium">{r.ok.toLocaleString()}</dd>
        <dt className="flex items-center gap-1.5">
          <span className="size-2 rounded-sm bg-destructive" />
          {t('overview.errors')}
        </dt>
        <dd className="text-right font-medium">{r.err.toLocaleString()}</dd>
        {total > 0 && (
          <>
            <dt className="mt-1 border-t pt-1 text-muted-foreground">{t('overview.u.tokensInOut')}</dt>
            <dd className="mt-1 border-t pt-1 text-right">
              {compact(r.input)} / {compact(r.output)}
            </dd>
            <dt className="text-muted-foreground">{t('overview.u.credits')}</dt>
            <dd className="text-right">{r.credits.toFixed(2)}</dd>
            <dt className="text-muted-foreground">{t('overview.u.avgLatency')}</dt>
            <dd className="text-right">{r.latency ? fmtMs(r.latency) : '–'}</dd>
          </>
        )}
      </dl>
    </div>
  )
}

/** Requests per bucket from GET /usage/series, successes and errors stacked. */
export function UsageChart({ range }: { range: OverviewRange }) {
  const { t, lang } = useI18n()
  const { hours, bucket } = OVERVIEW_RANGES[range]
  const series = useUsageSeries(hours, bucket)

  const data = useMemo(() => {
    const byTime = new Map<number, Row>()
    for (const p of series.data?.points ?? []) {
      const ts = Math.floor(Date.parse(p.time) / 1000)
      byTime.set(ts, {
        t: ts,
        label: '',
        ok: Math.max(0, p.requests - p.errors),
        err: p.errors,
        input: p.inputTokens,
        output: p.outputTokens,
        credits: p.credits,
        latency: p.avgLatencyMs,
      })
    }
    // Fill empty buckets so the axis is continuous (buckets are epoch-aligned server-side).
    const now = Math.floor(Date.now() / 1000)
    const end = Math.floor(now / bucket) * bucket
    const start = Math.floor((now - hours * 3600) / bucket) * bucket
    const rows: Row[] = []
    const fmt: Intl.DateTimeFormatOptions =
      bucket >= 86400 ? { month: 'short', day: 'numeric' } : bucket >= 3600 * 6 ? { month: 'short', day: 'numeric', hour: '2-digit' } : { hour: '2-digit', minute: '2-digit' }
    for (let ts = start; ts <= end; ts += bucket) {
      const r = byTime.get(ts) ?? { t: ts, label: '', ok: 0, err: 0, input: 0, output: 0, credits: 0, latency: 0 }
      r.label = new Date(ts * 1000).toLocaleString(lang, fmt)
      rows.push(r)
    }
    return rows
  }, [series.data, hours, bucket, lang])

  if (series.isLoading) return <Skeleton className="h-64 w-full" />
  const total = data.reduce((s, r) => s + r.ok + r.err, 0)
  if (!total) {
    return <EmptyState icon={Activity} title={t('overview.u.noTraffic')} description={t('overview.noTrafficHint')} />
  }
  return (
    <div className="h-64 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} margin={{ top: 8, right: 8, left: -24, bottom: 0 }} barCategoryGap="18%">
          <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 4" />
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            minTickGap={24}
            tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }}
          />
          <YAxis allowDecimals={false} tickLine={false} axisLine={false} tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }} />
          <Tooltip cursor={{ fill: 'var(--muted)', opacity: 0.6 }} content={<ChartTooltip />} />
          <Bar dataKey="ok" name={t('overview.success')} stackId="1" fill="var(--chart-1)" animationDuration={700} />
          <Bar dataKey="err" name={t('overview.errors')} stackId="1" fill="var(--destructive)" radius={[3, 3, 0, 0]} animationDuration={700} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
