import { useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/** Used / limit with a bar (no bar when unlimited). Shared by API & Keys and the key-holder usage page. */
export function UsageMeter({ label, used, limit, format }: { label: string; used: number; limit?: number; format: (n: number) => string }) {
  const { t } = useI18n()
  const pct = limit && limit > 0 ? Math.min(100, (100 * used) / limit) : null
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular">
          <span className="font-medium">{format(used)}</span>
          <span className="text-muted-foreground"> / {limit && limit > 0 ? format(limit) : t('keys.unlimited')}</span>
        </span>
      </div>
      <div className={cn('mt-1.5 h-1.5 overflow-hidden rounded-full', pct != null && 'bg-muted')} aria-hidden={pct == null}>
        {pct != null && (
          <div
            role="progressbar"
            aria-valuenow={Math.round(pct)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={label}
            className={cn(
              'h-full rounded-full transition-[width] duration-500',
              pct >= 95 ? 'bg-destructive' : pct >= 80 ? 'bg-warning' : 'bg-primary',
            )}
            style={{ width: `${pct}%` }}
          />
        )}
      </div>
    </div>
  )
}
