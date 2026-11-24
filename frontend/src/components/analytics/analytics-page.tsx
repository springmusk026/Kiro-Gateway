import { getRouteApi, Link } from '@tanstack/react-router'
import { ChartColumn, Database, RefreshCw, SearchX } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { EmptyState } from '@/components/app/empty-state'
import { PageHeader } from '@/components/app/page-header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ApiError } from '@/lib/api'
import { timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useAnalytics } from '@/lib/queries.analytics'
import { useStorageInfo } from '@/lib/queries.history'
import type { AnalyticsFilters } from '@/lib/types'
import { cn } from '@/lib/utils'
import { BreakdownCard } from './breakdown'
import { AnalyticsCharts, useSeriesRows } from './charts'
import { ErrorsCard } from './errors-card'
import { AnalyticsFilterBar } from './filters'
import { Heatmap } from './heatmap'
import { KpiGrid, KpiSkeleton, SecondaryStats } from './kpis'
import { ANALYTICS_RANGES, FILTER_DIMS, readStoredRange, storeRange, type AnalyticsRange, type FilterDim } from './lib'
import { SlowestCard } from './slowest'

const route = getRouteApi('/_app/analytics')

const tz = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
})()

function PageSkeleton() {
  return (
    <>
      <KpiSkeleton />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[330px] rounded-xl" />
        ))}
      </div>
      <Skeleton className="h-96 rounded-xl" />
    </>
  )
}

export function AnalyticsPage() {
  const { t, lang } = useI18n()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const storage = useStorageInfo()
  const noDb = storage.data ? !storage.data.database : false

  const [stored] = useState<AnalyticsRange>(() => (typeof window === 'undefined' ? '24h' : readStoredRange()))
  const range = search.range ?? stored
  const hours = ANALYTICS_RANGES[range]
  const filters: AnalyticsFilters = useMemo(
    () => ({ model: search.model, account: search.account, apiKey: search.apiKey, endpoint: search.endpoint }),
    [search.model, search.account, search.apiKey, search.endpoint],
  )
  const filtered = FILTER_DIMS.some((d) => filters[d])

  const report = useAnalytics(hours, filters, !noDb)
  // Filter options always come from the unfiltered report (shares the cache entry when no filter is active).
  const unfiltered = useAnalytics(hours, {}, !noDb && filtered)
  const data = report.data
  const rows = useSeriesRows(data, lang)

  const [, tick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 15_000)
    return () => clearInterval(id)
  }, [])

  const setSearch = (patch: Partial<typeof search>) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false })
  const onRange = (r: AnalyticsRange) => {
    storeRange(r)
    setSearch({ range: r })
  }
  const onFilter = (dim: FilterDim, v: string | undefined) => setSearch({ [dim]: v })
  const onClear = () => setSearch({ model: undefined, account: undefined, apiKey: undefined, endpoint: undefined })

  const conflict = report.error instanceof ApiError && report.error.status === 409
  const header = (
    <PageHeader
      title={t('nav.analytics')}
      description={t('page.analytics')}
      actions={
        data && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => report.refetch()} aria-label={t('analytics.refresh')}>
                {report.isFetching ? (
                  <RefreshCw className="animate-spin" />
                ) : (
                  <span className="relative mx-0.5 size-2 rounded-full bg-success text-success animate-pulse-ring" />
                )}
                <span className="tabular">{t('analytics.updated', { t: timeAgo(report.dataUpdatedAt / 1000, lang) })}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('analytics.live')}</TooltipContent>
          </Tooltip>
        )
      }
    />
  )

  if (noDb || conflict) {
    return (
      <div className="space-y-6">
        {header}
        <Card>
          <EmptyState
            icon={Database}
            title={t('analytics.noDb.title')}
            description={t('analytics.noDb.text')}
            action={
              <Button asChild variant="outline" size="sm">
                <Link to="/logs">{t('analytics.noDb.action')}</Link>
              </Button>
            }
          />
        </Card>
      </div>
    )
  }

  const filterBar = (
    <AnalyticsFilterBar
      range={range}
      onRange={onRange}
      filters={filters}
      onFilter={onFilter}
      onClear={onClear}
      options={filtered ? unfiltered.data : data}
    />
  )

  let body
  if (!data) {
    body = report.isError ? (
      <Card>
        <EmptyState
          icon={ChartColumn}
          title={t('analytics.failed')}
          description={report.error?.message}
          action={
            <Button variant="outline" size="sm" onClick={() => report.refetch()}>
              <RefreshCw />
              {t('common.retry')}
            </Button>
          }
        />
      </Card>
    ) : (
      <PageSkeleton />
    )
  } else if (data.summary.requests === 0) {
    body = (
      <Card>
        <EmptyState
          icon={filtered ? SearchX : ChartColumn}
          title={t('analytics.empty.title')}
          description={filtered ? t('analytics.empty.filtered') : t('analytics.empty.text')}
          action={
            filtered ? (
              <Button variant="outline" size="sm" onClick={onClear}>
                {t('analytics.clear')}
              </Button>
            ) : undefined
          }
        />
      </Card>
    )
  } else {
    body = (
      <div className={cn('space-y-4 transition-opacity duration-300', report.isPlaceholderData && 'opacity-60')} aria-busy={report.isFetching}>
        <KpiGrid s={data.summary} p={data.previous} hours={hours} range={range} />
        <SecondaryStats s={data.summary} p={data.previous} />
        <AnalyticsCharts rows={rows} bucketSec={data.bucketSec} />
        <BreakdownCard report={data} onFilter={(d, k) => onFilter(d, k)} />
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <ErrorsCard errors={data.errors ?? []} total={data.summary.requests} />
          <Heatmap cells={data.heatmap ?? []} tz={tz} />
        </div>
        <SlowestCard rows={data.slowest ?? []} />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {header}
      {filterBar}
      {body}
    </div>
  )
}
