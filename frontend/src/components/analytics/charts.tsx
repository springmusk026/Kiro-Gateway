import { useMemo, type ReactNode } from 'react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { fmtMs } from '@/components/logs/history-bits'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { compact } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsReport } from '@/lib/types'
import { bucketLabel, perBucket } from './lib'

export type SeriesRow = {
  t: number
  label: string
  long: string
  ok: number
  err: number
  throttled: number
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  credits: number
  p50: number | null
  p95: number | null
  ttfb: number | null
}

/** Continuous rows: empty buckets are omitted server-side, so fill them (buckets are epoch-aligned). */
export function useSeriesRows(report: AnalyticsReport | undefined, lang: string): SeriesRow[] {
  return useMemo(() => {
    if (!report) return []
    const bucket = report.bucketSec || 3600
    const byTime = new Map<number, SeriesRow>()
    for (const p of report.series ?? []) {
      const ts = Math.floor(Date.parse(p.time) / 1000)
      byTime.set(ts, {
        t: ts,
        label: '',
        long: '',
        ok: Math.max(0, p.requests - p.errors),
        err: p.errors,
        throttled: p.throttled,
        input: p.inputTokens,
        output: p.outputTokens,
        cacheRead: p.cacheReadTokens,
        cacheWrite: p.cacheWriteTokens,
        credits: p.credits,
        // A bucket with no successful request has no latency: leave a gap, not a zero.
        p50: p.p50LatencyMs || null,
        p95: p.p95LatencyMs || null,
        ttfb: p.p50TtfbMs || null,
      })
    }
    const from = Math.floor(Date.parse(report.from) / 1000)
    const to = Math.floor(Date.parse(report.to) / 1000)
    const start = Math.floor(from / bucket) * bucket
    const rows: SeriesRow[] = []
    for (let ts = start; ts < to; ts += bucket) {
      const r = byTime.get(ts) ?? {
        t: ts, label: '', long: '', ok: 0, err: 0, throttled: 0, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, credits: 0, p50: null, p95: null, ttfb: null,
      }
      r.label = bucketLabel(ts, bucket, lang)
      r.long = bucketLabel(ts, bucket, lang, true)
      rows.push(r)
    }
    return rows
  }, [report, lang])
}

const AXIS_TICK = { fill: 'var(--muted-foreground)', fontSize: 11 }
const GRID = <CartesianGrid vertical={false} stroke="var(--border)" />

type Key = { color: string; label: string; kind: 'line' | 'bar' }

/** Legend: a key mirroring the mark (short line for lines, square for bars), text in ink colours. */
function Legend({ items }: { items: Key[] }) {
  return (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {items.map((i) => (
        <li key={i.label} className="flex items-center gap-1.5">
          {i.kind === 'line' ? (
            <span className="h-0.5 w-3 rounded-full" style={{ background: i.color }} aria-hidden />
          ) : (
            <span className="size-2.5 rounded-[3px]" style={{ background: i.color }} aria-hidden />
          )}
          {i.label}
        </li>
      ))}
    </ul>
  )
}

type TipRow = { color: string; label: string; value: string; muted?: boolean }

function TipBox({ title, rows }: { title: string; rows: TipRow[] }) {
  return (
    <div className="min-w-[170px] rounded-lg border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
      <p className="mb-1.5 text-muted-foreground">{title}</p>
      <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-1 tabular">
        {rows.map((r) => (
          <div key={r.label} className="contents">
            <dd className={r.muted ? 'text-muted-foreground' : 'font-semibold'}>{r.value}</dd>
            <dt className="flex items-center justify-end gap-1.5 text-muted-foreground">
              {r.label}
              <span className="h-0.5 w-2.5 rounded-full" style={{ background: r.color }} aria-hidden />
            </dt>
          </div>
        ))}
      </dl>
    </div>
  )
}

function makeTip(build: (r: SeriesRow) => TipRow[]) {
  return function Tip({ active, payload }: { active?: boolean; payload?: readonly { payload?: unknown }[] }) {
    const r = payload?.[0]?.payload as SeriesRow | undefined
    if (!active || !r) return null
    return <TipBox title={r.long} rows={build(r)} />
  }
}

function ChartCard({ title, hint, legend, children }: { title: string; hint: string; legend?: Key[]; children: ReactNode }) {
  return (
    <Card className="min-w-0 gap-4">
      <CardHeader className="gap-1.5">
        <CardTitle>{title}</CardTitle>
        <CardDescription>{hint}</CardDescription>
        {legend && <div className="pt-1">{<Legend items={legend} />}</div>}
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full sm:h-60">
          <ResponsiveContainer width="100%" height="100%">
            {children as never}
          </ResponsiveContainer>
        </div>
      </CardContent>
    </Card>
  )
}

/** Stacked column segment: 4px rounded data-end only on the top segment, a 2px surface gap above the lower one. */
function StackShape(part: 'bottom' | 'top', other: (r: SeriesRow) => number) {
  return function Shape(props: unknown) {
    const { x, y, width, height, payload } = props as { x: number; y: number; width: number; height: number; payload: SeriesRow }
    if (!height || height <= 0) return <g />
    const isTop = part === 'top' || other(payload) === 0
    let h = height
    if (part === 'top' && other(payload) > 0) h = Math.max(0, height - 2)
    if (h <= 0) return <g />
    const r = isTop ? Math.min(4, width / 2, h) : 0
    const fill = part === 'top' ? 'var(--destructive)' : 'var(--chart-1)'
    const d = `M${x},${y + h} V${y + r} Q${x},${y} ${x + r},${y} H${x + width - r} Q${x + width},${y} ${x + width},${y + r} V${y + h} Z`
    return <path d={d} fill={fill} />
  }
}

function TopRounded(fill: string) {
  return function Shape(props: unknown) {
    const { x, y, width, height } = props as { x: number; y: number; width: number; height: number }
    if (!height || height <= 0) return <g />
    const r = Math.min(4, width / 2, height)
    const d = `M${x},${y + height} V${y + r} Q${x},${y} ${x + r},${y} H${x + width - r} Q${x + width},${y} ${x + width},${y + r} V${y + height} Z`
    return <path d={d} fill={fill} />
  }
}

const yCompact = (v: number) => compact(v)

export function AnalyticsCharts({ rows, bucketSec }: { rows: SeriesRow[]; bucketSec: number }) {
  const { t } = useI18n()
  const per = perBucket(t, bucketSec)
  const S = {
    ok: t('analytics.s.success'),
    err: t('analytics.s.errors'),
    input: t('analytics.s.input'),
    output: t('analytics.s.output'),
    cacheRead: t('analytics.s.cacheRead'),
    p50: t('analytics.s.p50'),
    p95: t('analytics.s.p95'),
    ttfb: t('analytics.s.ttfb'),
    credits: t('analytics.s.credits'),
  }

  const shapes = useMemo(
    () => ({
      ok: StackShape('bottom', (r) => r.err),
      err: StackShape('top', (r) => r.ok),
      credits: TopRounded('var(--chart-1)'),
    }),
    [],
  )

  const tips = useMemo(
    () => ({
      requests: makeTip((r) => [
        { color: 'var(--chart-1)', label: S.ok, value: r.ok.toLocaleString() },
        { color: 'var(--destructive)', label: S.err, value: r.err.toLocaleString() },
        ...(r.throttled ? [{ color: 'transparent', label: t('analytics.s.throttled'), value: r.throttled.toLocaleString(), muted: true }] : []),
        { color: 'transparent', label: t('analytics.s.total'), value: (r.ok + r.err).toLocaleString(), muted: true },
      ]),
      tokens: makeTip((r) => [
        { color: 'var(--chart-1)', label: S.input, value: r.input.toLocaleString() },
        { color: 'var(--chart-2)', label: S.output, value: r.output.toLocaleString() },
        { color: 'var(--chart-3)', label: S.cacheRead, value: r.cacheRead.toLocaleString() },
        ...(r.cacheWrite ? [{ color: 'transparent', label: t('analytics.s.cacheWrite'), value: r.cacheWrite.toLocaleString(), muted: true }] : []),
      ]),
      latency: makeTip((r) => [
        { color: 'var(--chart-1)', label: S.p50, value: r.p50 ? fmtMs(r.p50) : '–' },
        { color: 'var(--chart-2)', label: S.p95, value: r.p95 ? fmtMs(r.p95) : '–' },
        { color: 'var(--chart-3)', label: S.ttfb, value: r.ttfb ? fmtMs(r.ttfb) : '–' },
      ]),
      credits: makeTip((r) => [{ color: 'var(--chart-1)', label: S.credits, value: r.credits.toFixed(2) }]),
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t],
  )

  const xAxis = <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={28} tick={AXIS_TICK} />
  const margin = { top: 6, right: 6, left: 0, bottom: 0 }
  const lineProps = {
    type: 'monotone' as const,
    strokeWidth: 2,
    dot: false,
    connectNulls: false,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    animationDuration: 600,
  }
  const activeDot = (color: string) => ({ r: 4, fill: color, stroke: 'var(--card)', strokeWidth: 2 })
  const cursorLine = { stroke: 'var(--muted-foreground)', strokeOpacity: 0.4, strokeWidth: 1 }

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <ChartCard
        title={t('analytics.chart.requests')}
        hint={t('analytics.chart.requestsHint', { per })}
        legend={[
          { color: 'var(--chart-1)', label: S.ok, kind: 'bar' },
          { color: 'var(--destructive)', label: S.err, kind: 'bar' },
        ]}
      >
        <BarChart data={rows} margin={margin} barCategoryGap="20%">
          {GRID}
          {xAxis}
          <YAxis allowDecimals={false} tickLine={false} axisLine={false} tick={AXIS_TICK} width={40} tickFormatter={yCompact} />
          <Tooltip cursor={{ fill: 'var(--muted)', opacity: 0.6 }} content={tips.requests} />
          <Bar dataKey="ok" name={S.ok} stackId="r" fill="var(--chart-1)" maxBarSize={24} shape={shapes.ok} animationDuration={600} />
          <Bar dataKey="err" name={S.err} stackId="r" fill="var(--destructive)" maxBarSize={24} shape={shapes.err} animationDuration={600} />
        </BarChart>
      </ChartCard>

      <ChartCard
        title={t('analytics.chart.tokens')}
        hint={t('analytics.chart.tokensHint', { per })}
        legend={[
          { color: 'var(--chart-1)', label: S.input, kind: 'line' },
          { color: 'var(--chart-2)', label: S.output, kind: 'line' },
          { color: 'var(--chart-3)', label: S.cacheRead, kind: 'line' },
        ]}
      >
        <LineChart data={rows} margin={margin}>
          {GRID}
          {xAxis}
          <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={40} tickFormatter={yCompact} />
          <Tooltip cursor={cursorLine} content={tips.tokens} />
          <Line {...lineProps} dataKey="input" name={S.input} stroke="var(--chart-1)" activeDot={activeDot('var(--chart-1)')} />
          <Line {...lineProps} dataKey="output" name={S.output} stroke="var(--chart-2)" activeDot={activeDot('var(--chart-2)')} />
          <Line {...lineProps} dataKey="cacheRead" name={S.cacheRead} stroke="var(--chart-3)" activeDot={activeDot('var(--chart-3)')} />
        </LineChart>
      </ChartCard>

      <ChartCard
        title={t('analytics.chart.latency')}
        hint={t('analytics.chart.latencyHint')}
        legend={[
          { color: 'var(--chart-1)', label: S.p50, kind: 'line' },
          { color: 'var(--chart-2)', label: S.p95, kind: 'line' },
          { color: 'var(--chart-3)', label: S.ttfb, kind: 'line' },
        ]}
      >
        <LineChart data={rows} margin={margin}>
          {GRID}
          {xAxis}
          <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={44} tickFormatter={(v: number) => fmtMs(v)} />
          <Tooltip cursor={cursorLine} content={tips.latency} />
          <Line {...lineProps} dataKey="p50" name={S.p50} stroke="var(--chart-1)" activeDot={activeDot('var(--chart-1)')} />
          <Line {...lineProps} dataKey="p95" name={S.p95} stroke="var(--chart-2)" activeDot={activeDot('var(--chart-2)')} />
          <Line {...lineProps} dataKey="ttfb" name={S.ttfb} stroke="var(--chart-3)" activeDot={activeDot('var(--chart-3)')} />
        </LineChart>
      </ChartCard>

      <ChartCard title={t('analytics.chart.credits')} hint={t('analytics.chart.creditsHint', { per })}>
        <BarChart data={rows} margin={margin} barCategoryGap="20%">
          {GRID}
          {xAxis}
          <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={40} tickFormatter={yCompact} />
          <Tooltip cursor={{ fill: 'var(--muted)', opacity: 0.6 }} content={tips.credits} />
          <Bar dataKey="credits" name={S.credits} fill="var(--chart-1)" maxBarSize={24} shape={shapes.credits} animationDuration={600} />
        </BarChart>
      </ChartCard>
    </div>
  )
}
