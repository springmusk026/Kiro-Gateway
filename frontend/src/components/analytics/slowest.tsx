import { Link } from '@tanstack/react-router'
import { ArrowRight, Timer } from 'lucide-react'
import { EmptyState } from '@/components/app/empty-state'
import { fmtMs, unix } from '@/components/logs/history-bits'
import { EndpointChip, StatusBadge } from '@/components/logs/log-meta'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { compact, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsSlow } from '@/lib/types'

export function SlowestCard({ rows }: { rows: AnalyticsSlow[] }) {
  const { t, lang } = useI18n()
  const max = rows.reduce((m, r) => Math.max(m, r.durationMs), 0)
  return (
    <Card className="min-w-0">
      <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-3">
        <div className="space-y-1.5">
          <CardTitle>{t('analytics.slowest')}</CardTitle>
          <CardDescription>{t('analytics.slowestHint')}</CardDescription>
        </div>
        <Button asChild variant="ghost" size="sm" className="group -mr-2 text-muted-foreground">
          <Link to="/logs">
            {t('analytics.viewLogs')}
            <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </Link>
        </Button>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <EmptyState icon={Timer} title={t('analytics.slowestNone')} />
        ) : (
          <ul className="-mx-2 divide-y">
            {rows.map((r) => (
              <li key={r.id}>
                <Link
                  to="/logs"
                  title={t('analytics.viewLogs')}
                  className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto]"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate font-mono text-[13px]" title={r.model}>
                      {r.model || '–'}
                    </span>
                    <span className="hidden shrink-0 md:inline-flex">
                      <EndpointChip endpoint={r.endpoint} />
                    </span>
                  </div>
                  <div className="flex items-center justify-end gap-2 tabular md:justify-start">
                    <span className="font-semibold">{fmtMs(r.durationMs)}</span>
                    <span className="hidden h-1 flex-1 overflow-hidden rounded-full bg-muted md:block" aria-hidden>
                      <span className="block h-full rounded-full bg-chart-1" style={{ width: `${max ? (100 * r.durationMs) / max : 0}%` }} />
                    </span>
                  </div>
                  <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground tabular md:justify-end">
                    <span className="md:hidden">
                      <EndpointChip endpoint={r.endpoint} />
                    </span>
                    <span>
                      {t('analytics.col.ttfb')} {r.ttfbMs ? fmtMs(r.ttfbMs) : '–'}
                    </span>
                    <span>
                      {t('analytics.col.output')} {compact(r.outputTokens)}
                    </span>
                    {r.attempts > 1 && <span>{t('analytics.attemptsN', { n: r.attempts })}</span>}
                  </div>
                  <div className="flex items-center justify-end gap-2">
                    <span className="max-sm:hidden">
                      <StatusBadge log={r} />
                    </span>
                    {r.status !== 'success' && (
                      <span className="sm:hidden">
                        <StatusBadge log={r} />
                      </span>
                    )}
                    <span className="whitespace-nowrap text-right text-xs text-muted-foreground sm:w-20" title={new Date(r.time).toLocaleString(lang)}>
                      {timeAgo(unix(r.time), lang)}
                    </span>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
