import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { compact } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { RequestLog } from '@/lib/types'
import { cn } from '@/lib/utils'

function Metric({ label, value, format, tone }: { label: string; value: number; format: (n: number) => string; tone?: string }) {
  const n = useAnimatedNumber(value)
  return (
    <div className="min-w-0 px-4 py-3 sm:px-5">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className={cn('mt-1 text-xl font-semibold tracking-tight tabular', tone)}>{format(n)}</p>
    </div>
  )
}

export function SummaryStrip({ logs, loading, filtered, total }: { logs: RequestLog[]; loading: boolean; filtered: boolean; total: number }) {
  const { t } = useI18n()
  if (loading) return <Skeleton className="h-[74px] w-full rounded-xl" />
  const n = logs.length
  const ok = logs.filter((l) => l.status === 'success')
  const timed = ok.filter((l) => l.duration > 0)
  const avg = timed.length ? timed.reduce((s, l) => s + l.duration, 0) / timed.length : 0
  const tokens = logs.reduce((s, l) => s + (l.tokens || 0), 0)
  const credits = logs.reduce((s, l) => s + (l.credits || 0), 0)
  const rate = n ? (100 * ok.length) / n : 0
  return (
    <Card className="grid grid-cols-2 gap-0 divide-border py-0 shadow-xs sm:grid-cols-3 lg:grid-cols-5 lg:divide-x">
      <Metric
        label={filtered ? t('logs.sumRequestsFiltered', { total }) : t('logs.sumRequests')}
        value={n}
        format={(v) => Math.round(v).toLocaleString()}
      />
      <Metric
        label={t('logs.sumSuccess')}
        value={rate}
        format={(v) => (n ? v.toFixed(1) + '%' : '–')}
        tone={!n ? undefined : rate >= 95 ? 'text-success' : rate >= 80 ? 'text-warning' : 'text-destructive'}
      />
      <Metric
        label={t('logs.sumLatency')}
        value={avg}
        format={(v) => (timed.length ? (v >= 1000 ? (v / 1000).toFixed(2) + 's' : Math.round(v) + 'ms') : '–')}
      />
      <Metric label={t('logs.sumTokens')} value={tokens} format={(v) => compact(Math.round(v))} />
      <Metric label={t('logs.sumCredits')} value={credits} format={(v) => v.toFixed(2)} />
    </Card>
  )
}
