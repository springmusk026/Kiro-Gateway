import { useMemo, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsHeatCell } from '@/lib/types'

/** Monday-first weekday order (backend uses 0 = Sunday). */
const ORDER = [1, 2, 3, 4, 5, 6, 0]
const STEPS = [0, 0.25, 0.5, 0.75, 1]

/** Sequential single hue: --chart-1 mixed into the card surface by intensity. */
const shade = (f: number) => `color-mix(in oklab, var(--chart-1) ${Math.round(f * 100)}%, var(--card))`

export function Heatmap({ cells, tz }: { cells: AnalyticsHeatCell[]; tz: string }) {
  const { t, lang } = useI18n()
  const [hover, setHover] = useState<{ w: number; h: number; x: number; y: number } | null>(null)

  const { grid, max } = useMemo(() => {
    const g = new Map<string, AnalyticsHeatCell>()
    let m = 0
    for (const c of cells) {
      g.set(`${c.weekday}-${c.hour}`, c)
      m = Math.max(m, c.requests)
    }
    return { grid: g, max: m }
  }, [cells])

  const dayName = useMemo(() => {
    // 2024-01-07 was a Sunday.
    const names: string[] = []
    for (let d = 0; d < 7; d++) names.push(new Date(Date.UTC(2024, 0, 7 + d)).toLocaleDateString(lang, { weekday: 'short', timeZone: 'UTC' }))
    return names
  }, [lang])

  const intensity = (n: number) => (n > 0 && max > 0 ? 0.2 + 0.8 * Math.sqrt(n / max) : 0)
  const hovered = hover ? grid.get(`${hover.w}-${hover.h}`) : undefined
  const pad = (h: number) => String(h).padStart(2, '0')

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('analytics.heatmap')}</CardTitle>
        <CardDescription>{t('analytics.heatmapHint', { tz })}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="relative" onMouseLeave={() => setHover(null)}>
          <div
            className="grid gap-[2px] sm:gap-[3px]"
            style={{ gridTemplateColumns: 'auto repeat(24, minmax(0, 1fr))' }}
            role="grid"
            aria-label={t('analytics.heatmap')}
          >
            {ORDER.map((w) => (
              <div key={w} className="contents" role="row">
                <div className="pr-1.5 text-[11px] leading-none text-muted-foreground self-center" role="rowheader">
                  {dayName[w]}
                </div>
                {Array.from({ length: 24 }, (_, h) => {
                  const c = grid.get(`${w}-${h}`)
                  const n = c?.requests ?? 0
                  const active = hover?.w === w && hover?.h === h
                  return (
                    <div
                      key={h}
                      role="gridcell"
                      aria-label={t('analytics.cell', { day: dayName[w], hour: pad(h), n, e: c?.errors ?? 0 })}
                      className="aspect-square rounded-[3px] transition-[outline-color] outline outline-1 -outline-offset-1"
                      style={{
                        background: n ? shade(intensity(n)) : 'var(--muted)',
                        outlineColor: active ? 'var(--foreground)' : 'transparent',
                      }}
                      onMouseEnter={(e) => {
                        const cell = e.currentTarget.getBoundingClientRect()
                        const box = e.currentTarget.parentElement!.parentElement!.getBoundingClientRect()
                        const x = cell.left - box.left + cell.width / 2
                        setHover({ w, h, x: Math.min(Math.max(x, 90), box.width - 90), y: cell.top - box.top })
                      }}
                    />
                  )
                })}
              </div>
            ))}
            <div />
            {Array.from({ length: 24 }, (_, h) => (
              <div key={h} className="pt-1 text-center text-[10px] leading-none text-muted-foreground tabular">
                {h % 3 === 0 && <span className={h % 6 === 0 ? '' : 'max-sm:invisible'}>{pad(h)}</span>}
              </div>
            ))}
          </div>
          {hover && (
            <div
              className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full whitespace-nowrap rounded-lg border bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md tabular"
              style={{ left: hover.x, top: hover.y - 6 }}
            >
              <p className="text-muted-foreground">
                {dayName[hover.w]} {pad(hover.h)}:00–{pad(hover.h)}:59
              </p>
              <p>
                <span className="font-semibold">{(hovered?.requests ?? 0).toLocaleString()}</span>{' '}
                <span className="text-muted-foreground">{t('analytics.cellRequests')}</span>
                {' · '}
                <span className="font-semibold">{(hovered?.errors ?? 0).toLocaleString()}</span>{' '}
                <span className="text-muted-foreground">{t('analytics.cellErrors')}</span>
              </p>
            </div>
          )}
        </div>
        <div className="mt-4 flex items-center justify-end gap-1.5 text-[11px] text-muted-foreground">
          <span>{t('analytics.less')}</span>
          <span className="size-3 rounded-[3px] bg-muted" aria-hidden />
          {STEPS.map((s) => (
            <span key={s} className="size-3 rounded-[3px]" style={{ background: shade(0.2 + 0.8 * s) }} aria-hidden />
          ))}
          <span>{t('analytics.more')}</span>
          {max > 0 && <span className="ml-1 tabular">{t('analytics.peak', { n: max.toLocaleString() })}</span>}
        </div>
      </CardContent>
    </Card>
  )
}
