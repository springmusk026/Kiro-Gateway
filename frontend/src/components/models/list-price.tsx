import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, usdRate } from '@/lib/format'
import type { TFn } from '@/lib/i18n'
import { useI18n } from '@/lib/i18n'
import type { ListPrice } from '@/lib/types'
import { cn } from '@/lib/utils'

/** "$3 / $15": input / output per million tokens. */
export const priceText = (p: ListPrice) => `${usdRate(p.input)} / ${usdRate(p.output)}`

/** Tooltip lines: input/output, cache prices, context tiers and the models.dev id. */
export function priceLines(t: TFn, p: ListPrice, source?: string): string[] {
  const lines = [t('pricing.inOut', { in: usdRate(p.input), out: usdRate(p.output) })]
  const cache = [
    p.cacheRead ? t('pricing.cacheRead', { v: usdRate(p.cacheRead) }) : '',
    p.cacheWrite ? t('pricing.cacheWrite', { v: usdRate(p.cacheWrite) }) : '',
  ].filter(Boolean)
  if (cache.length) lines.push(cache.join(' · '))
  for (const tier of p.tiers ?? []) {
    lines.push(t('pricing.tier', { n: compact(tier.above), in: usdRate(tier.input), out: usdRate(tier.output) }))
  }
  if (source) lines.push(t('pricing.from', { id: source }))
  return lines
}

/** A model's API list price with the details in a tooltip; a dash when models.dev has none. */
export function ListPriceCell({ price, source, className }: { price?: ListPrice; source?: string; className?: string }) {
  const { t } = useI18n()
  if (!price) {
    return (
      <span className={cn('text-muted-foreground', className)} title={t('pricing.none')}>
        –
      </span>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className={cn('cursor-default whitespace-nowrap tabular underline decoration-dotted decoration-muted-foreground/50 underline-offset-4', className)}>
          {priceText(price)}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <ul className="space-y-0.5">
          {priceLines(t, price, source).map((l) => (
            <li key={l}>{l}</li>
          ))}
        </ul>
      </TooltipContent>
    </Tooltip>
  )
}
