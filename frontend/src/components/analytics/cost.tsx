import { Link } from '@tanstack/react-router'
import { Info } from 'lucide-react'
import type { ReactNode } from 'react'
import { Card } from '@/components/ui/card'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { timeAgo, usd } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { kiroCost, pricesFetchedAt, pricesLoaded } from '@/lib/pricing'
import type { AnalyticsReport } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Delta } from './kpis'

function Block({
  label,
  value,
  known = true,
  delta,
  hint,
  tone,
  className,
}: {
  label: string
  value: number
  known?: boolean
  delta?: ReactNode
  hint?: ReactNode
  tone?: 'success'
  className?: string
}) {
  const n = useAnimatedNumber(value)
  return (
    <div className={cn('min-w-0 bg-card px-4 py-3.5 sm:px-5', className)}>
      <p className="truncate text-[13px] font-medium text-muted-foreground">{label}</p>
      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <p className={cn('text-xl font-semibold tracking-tight tabular sm:text-2xl', tone === 'success' && known && value > 0 && 'text-success')}>
          {known ? usd(n) : '—'}
        </p>
        {known && delta}
      </div>
      {hint && <div className="mt-1 truncate text-xs text-muted-foreground tabular">{hint}</div>}
    </div>
  )
}

/**
 * API list price vs what Kiro charged (credits × credit price) for the window, with
 * where the list prices come from. Kiro's spend is computed here; the server stores only credits.
 */
export function CostStrip({ report }: { report: AnalyticsReport }) {
  const { t, lang } = useI18n()
  const s = report.summary
  const p = report.previous
  const price = report.creditPriceUsd
  const loaded = pricesLoaded(report.pricing)
  const kiro = kiroCost(s.credits, price)
  const prevKiro = kiroCost(p.credits, price)
  const list = s.costUsd ?? 0
  const saved = list - kiro
  const share = list > 0 ? Math.abs(saved) / list : 0
  const fetched = pricesFetchedAt(report.pricing)

  const source = !report.pricing?.enabled && report.pricing
    ? t('pricing.off')
    : loaded
      ? t('pricing.source', { ago: fetched ? timeAgo(fetched, lang) : '–' })
      : t('pricing.notLoaded')

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="grid grid-cols-2 gap-px bg-border sm:grid-cols-3">
        <Block
          label={t('pricing.listPrice')}
          value={list}
          known={loaded}
          delta={<Delta cur={list} prev={p.costUsd ?? 0} good="neutral" />}
          hint={loaded ? t('pricing.listHint', { n: s.requests ? usd(list / s.requests) : '–' }) : undefined}
        />
        <Block
          label={t('pricing.kiroCost')}
          value={kiro}
          delta={<Delta cur={kiro} prev={prevKiro} good="neutral" />}
          hint={t('pricing.kiroHint', {
            credits: s.credits.toLocaleString(lang, { maximumFractionDigits: s.credits >= 100 ? 0 : 2 }),
            price: usd(kiroCost(1, price)),
          })}
        />
        <Block
          label={t('pricing.saved')}
          value={saved}
          known={loaded}
          tone="success"
          className="col-span-2 sm:col-span-1"
          hint={loaded && list > 0 ? t(saved >= 0 ? 'pricing.savedHint' : 'pricing.overHint', { pct: (share * 100).toFixed(1) + '%' }) : undefined}
        />
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-4 py-2 text-xs text-muted-foreground sm:px-5">
        <span className="flex min-w-0 items-center gap-1.5">
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="button" className="shrink-0 rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={t('pricing.note')}>
                <Info className="size-3.5" aria-hidden />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">{t('pricing.note')}</TooltipContent>
          </Tooltip>
          <span className="min-w-0">{source}</span>
        </span>
        <Link to="/settings" hash="budgets" className="ml-auto shrink-0 font-medium text-primary underline-offset-4 hover:underline">
          {t('pricing.creditPriceLink')} {usd(kiroCost(1, price))}
        </Link>
      </div>
    </Card>
  )
}
