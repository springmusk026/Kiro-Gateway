import { X } from 'lucide-react'
import { useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsFilters, AnalyticsReport } from '@/lib/types'
import { ANALYTICS_RANGES, FILTER_DIMS, useDimLabels, type AnalyticsRange, type FilterDim } from './lib'

const ALL = '__all__'

/** Range toggle + dimension selects in one row. Options come from the unfiltered report. */
export function AnalyticsFilterBar({
  range,
  onRange,
  filters,
  onFilter,
  onClear,
  options,
}: {
  range: AnalyticsRange
  onRange: (r: AnalyticsRange) => void
  filters: AnalyticsFilters
  onFilter: (dim: FilterDim, value: string | undefined) => void
  onClear: () => void
  options: AnalyticsReport | undefined
}) {
  const { t } = useI18n()
  const label = useDimLabels(t)
  const active = FILTER_DIMS.some((d) => filters[d])

  const opts = useMemo(() => {
    const out = {} as Record<FilterDim, { value: string; text: string }[]>
    for (const d of FILTER_DIMS) {
      const rows = (options?.breakdowns?.[d] ?? []).filter((g) => g.key)
      const list = rows.map((g) => ({ value: g.key, text: label(d, g.key, g.label).text }))
      // A value from a shared link may not be in this window's top rows: keep it selectable.
      const cur = filters[d]
      if (cur && !list.some((o) => o.value === cur)) list.unshift({ value: cur, text: label(d, cur).text })
      out[d] = list
    }
    return out
  }, [options, filters, label])

  return (
    <div className="flex flex-wrap items-center gap-2">
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        value={range}
        onValueChange={(v) => v && onRange(v as AnalyticsRange)}
        aria-label={t('analytics.range')}
      >
        {(Object.keys(ANALYTICS_RANGES) as AnalyticsRange[]).map((r) => (
          <ToggleGroupItem key={r} value={r} className="px-3 text-xs tabular">
            {t(`analytics.range.${r}` as const)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <div className="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:flex-wrap">
        {FILTER_DIMS.map((d) => (
          <Select key={d} value={filters[d] ?? ALL} onValueChange={(v) => onFilter(d, v === ALL ? undefined : v)}>
            <SelectTrigger
              size="sm"
              className="w-full min-w-0 sm:w-[168px] data-[active=true]:border-primary/50 data-[active=true]:bg-primary/[0.04]"
              data-active={!!filters[d]}
              aria-label={t(`analytics.filter.${d}Label` as const)}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="max-h-80">
              <SelectItem value={ALL}>{t(`analytics.filter.${d}` as const)}</SelectItem>
              {opts[d].map((o) => (
                <SelectItem key={o.value} value={o.value} className={d === 'model' || d === 'apiKey' ? 'font-mono text-[13px]' : undefined}>
                  {o.text}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ))}
      </div>
      {active && (
        <Button variant="ghost" size="sm" onClick={onClear} className="text-muted-foreground">
          <X />
          {t('analytics.clear')}
        </Button>
      )}
    </div>
  )
}
