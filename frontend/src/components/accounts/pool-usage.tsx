import { Coins, Gauge } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useI18n } from '@/lib/i18n'
import { useBudgets } from '@/lib/queries.cost'
import { cn } from '@/lib/utils'
import { UsageBar } from './account-bits'

const fmt = (n: number) => (n >= 10000 ? Math.round(n).toLocaleString() : n.toFixed(n < 100 ? 1 : 0))

/** Pool-wide credits left in the current period (Accounts page). */
export function PoolUsageCard() {
  const { t } = useI18n()
  const { data, isLoading } = useBudgets()
  if (isLoading) return <Skeleton className="h-[86px] w-full rounded-xl" />
  const p = data?.pool
  if (!p) return null
  return (
    <Card className="gap-0 px-5 py-4 shadow-xs animate-rise">
      <div className="flex flex-wrap items-start gap-x-6 gap-y-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground">
            <Coins className="size-[18px]" />
          </div>
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">{t('cost.poolTitle')}</p>
            {p.withLimit ? (
              <p className="text-lg font-semibold tabular leading-tight">
                {t('cost.poolRemaining', { remaining: fmt(p.remaining), limit: fmt(p.limit) })}
              </p>
            ) : (
              <p className="text-sm text-muted-foreground">{t('cost.poolUnknown')}</p>
            )}
          </div>
        </div>
        {p.withLimit > 0 && (
          <div className="min-w-[220px] flex-1 space-y-1.5 sm:pt-1">
            <UsageBar fraction={p.ratio} />
            <p className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
              <span className="tabular">{t('cost.poolUsed', { pct: (p.ratio * 100).toFixed(1), n: p.withLimit })}</span>
              {p.saturated > 0 && (
                <span className={cn('inline-flex items-center gap-1 text-warning')}>
                  <Gauge className="size-3" />
                  {t('cost.poolSaturated', { n: p.saturated, pct: Math.round(p.threshold * 100) })}
                </span>
              )}
              {p.nextResetDay && <span>{t('cost.poolReset', { date: p.nextResetDay })}</span>}
            </p>
          </div>
        )}
      </div>
    </Card>
  )
}
