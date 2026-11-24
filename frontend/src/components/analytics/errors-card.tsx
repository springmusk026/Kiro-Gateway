import { Link } from '@tanstack/react-router'
import { ArrowRight, CircleCheck } from 'lucide-react'
import { TablePagination } from '@/components/app/table-pagination'
import { usePagination } from '@/hooks/use-pagination'
import { EmptyState } from '@/components/app/empty-state'
import { unix } from '@/components/logs/history-bits'
import { ErrorTypeBadge } from '@/components/logs/log-meta'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { AnalyticsError } from '@/lib/types'

export function ErrorsCard({ errors, total }: { errors: AnalyticsError[]; total: number }) {
  const { t, lang } = useI18n()
  const sum = errors.reduce((s, e) => s + e.count, 0)
  const pg = usePagination(errors, { storageKey: 'analytics-errors', pageSize: 10 })
  return (
    <Card className="min-w-0">
      <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-3">
        <div className="space-y-1.5">
          <CardTitle>{t('analytics.errors')}</CardTitle>
          <CardDescription>{t('analytics.errorsHint')}</CardDescription>
        </div>
        <Button asChild variant="ghost" size="sm" className="group -mr-2 text-muted-foreground">
          <Link to="/logs">
            {t('analytics.viewLogs')}
            <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </Link>
        </Button>
      </CardHeader>
      <CardContent>
        {errors.length === 0 ? (
          <EmptyState icon={CircleCheck} title={t('analytics.errorsNone')} description={total ? t('analytics.errorsNoneHint') : undefined} />
        ) : (
          <>
          <ul className="-my-2 divide-y">
            {pg.pageItems.map((e) => (
              <li key={`${e.type}-${e.status}`} className="py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <ErrorTypeBadge type={e.type || 'unknown'} />
                  <Badge variant="outline" className="font-mono font-normal text-muted-foreground">
                    {e.status || '–'}
                  </Badge>
                  <span className="ml-auto flex items-baseline gap-2 tabular">
                    <span className="font-semibold">{e.count.toLocaleString()}</span>
                    <span className="text-xs text-muted-foreground">{sum ? ((100 * e.count) / sum).toFixed(0) + '%' : ''}</span>
                  </span>
                </div>
                <div className="mt-1.5 flex items-start gap-3 text-xs">
                  <p className="min-w-0 flex-1 break-words font-mono text-muted-foreground line-clamp-2" title={e.sample}>
                    {e.sample || '–'}
                  </p>
                  <span className="shrink-0 text-muted-foreground" title={new Date(e.lastSeen).toLocaleString(lang)}>
                    {timeAgo(unix(e.lastSeen), lang)}
                  </span>
                </div>
              </li>
            ))}
          </ul>
          <TablePagination className="-mx-6 -mb-6 mt-2" page={pg.page} pageCount={pg.pageCount} pageSize={pg.pageSize} total={pg.total} onPage={pg.setPage} onPageSize={pg.setPageSize} />
          </>
        )}
      </CardContent>
    </Card>
  )
}
