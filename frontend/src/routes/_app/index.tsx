import { createFileRoute, Link } from '@tanstack/react-router'
import { Activity, ArrowRight, CheckCircle2, Coins, ShieldAlert, Users } from 'lucide-react'
import { useMemo, useState } from 'react'
import { CodeBlock } from '@/components/app/code-block'
import { CostAlertsBanner } from '@/components/app/cost-alerts'
import { StatCard } from '@/components/app/stat-card'
import { ActivityFeed } from '@/components/overview/activity-feed'
import { TopModels } from '@/components/overview/top-models'
import { TrafficChart } from '@/components/overview/traffic-chart'
import { UsageBreakdown } from '@/components/overview/usage-breakdown'
import { OVERVIEW_RANGES, UsageChart, type OverviewRange } from '@/components/overview/usage-chart'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { compact, duration } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useLogs, useModels, useStatus } from '@/lib/queries'
import { useStorageInfo } from '@/lib/queries.history'

export const Route = createFileRoute('/_app/')({ component: OverviewPage })

function greetingKey() {
  const h = new Date().getHours()
  return h < 12 ? 'overview.greetingMorning' : h < 18 ? 'overview.greetingAfternoon' : 'overview.greetingEvening'
}

const RANGE_KEY = 'kiro_overview_range'
function readRange(): OverviewRange {
  try {
    const v = localStorage.getItem(RANGE_KEY)
    if (v && v in OVERVIEW_RANGES) return v as OverviewRange
  } catch {
    /* storage unavailable */
  }
  return '24h'
}

function OverviewPage() {
  const { t } = useI18n()
  const { data: status } = useStatus()
  const { data: logs = [] } = useLogs(10_000)
  const { data: catalog } = useModels()
  const { data: storage } = useStorageInfo()
  const db = !!storage?.database
  const [range, setRange] = useState<OverviewRange>(() => readRange())
  const changeRange = (v: OverviewRange) => {
    setRange(v)
    try {
      localStorage.setItem(RANGE_KEY, v)
    } catch {
      /* storage unavailable */
    }
  }

  const avgLatency = useMemo(() => {
    const ok = logs.filter((l) => l.status === 'success' && l.duration > 0)
    return ok.length ? ok.reduce((s, l) => s + l.duration, 0) / ok.length : 0
  }, [logs])

  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const sample = catalog?.models.find((m) => m.enabled && m.id.includes('sonnet'))?.id ?? 'claude-sonnet-4.5'
  const snippet = `export ANTHROPIC_BASE_URL=${origin}\nexport ANTHROPIC_AUTH_TOKEN=<your-api-key>\nexport ANTHROPIC_MODEL=${sample}\nclaude`

  const total = status?.totalRequests ?? 0
  const success = status?.successRequests ?? 0

  return (
    <div className="space-y-6">
      <div className="relative -mx-4 -mt-6 overflow-hidden px-4 pb-2 pt-6 surface-glow lg:-mx-8 lg:-mt-8 lg:px-8 lg:pt-8">
        <h1 className="text-2xl font-semibold tracking-tight">{t(greetingKey())}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('overview.subtitle')}</p>
      </div>

      {status?.apiOpen && (
        <Alert className="border-warning/40 bg-warning/10 animate-rise">
          <ShieldAlert className="size-4 !text-warning" />
          <AlertTitle>{t('overview.openTitle')}</AlertTitle>
          <AlertDescription className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span>{t('overview.openText')}</span>
            <Button asChild size="sm" variant="outline" className="shrink-0">
              <Link to="/settings">{t('overview.openAction')}</Link>
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <CostAlertsBanner />

      {!status ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[118px] rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4 stagger">
          <StatCard
            label={t('overview.accounts')}
            value={status.available}
            icon={Users}
            tone={status.available > 0 ? 'default' : 'danger'}
            hint={t('overview.ofTotal', { n: status.accounts })}
          />
          <StatCard
            label={t('overview.requests')}
            value={total}
            icon={Activity}
            hint={
              <span>
                {t('overview.uptime', { d: duration(status.uptime) })}
                {avgLatency > 0 && <> · {t('overview.latency', { ms: (avgLatency / 1000).toFixed(1) + 's' })}</>}
              </span>
            }
          />
          <StatCard
            label={t('overview.successRate')}
            value={total ? (100 * success) / total : 100}
            format={(n) => (total ? n.toFixed(1) + '%' : '—')}
            icon={CheckCircle2}
            tone={total && success / total < 0.9 ? 'danger' : 'success'}
            hint={t('overview.failed', { n: status.failedRequests })}
          >
            {total > 0 && (
              <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-destructive/20">
                <div className="h-full rounded-full bg-success transition-[width] duration-700" style={{ width: `${(100 * success) / total}%` }} />
              </div>
            )}
          </StatCard>
          <StatCard
            label={t('overview.tokens')}
            value={status.totalTokens}
            format={(n) => compact(Math.round(n))}
            icon={Coins}
            hint={t('overview.credits', { n: status.totalCredits.toFixed(1) })}
          />
        </div>
      )}

      {db ? (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
          <Card className="min-w-0">
            <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-3">
              <div className="space-y-1.5">
                <CardTitle>{t('overview.traffic')}</CardTitle>
                <CardDescription>{t(`overview.u.trafficHint.${range}` as const)}</CardDescription>
              </div>
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                value={range}
                onValueChange={(v) => v && changeRange(v as OverviewRange)}
                aria-label={t('overview.u.range')}
              >
                {(Object.keys(OVERVIEW_RANGES) as OverviewRange[]).map((r) => (
                  <ToggleGroupItem key={r} value={r} className="px-3 text-xs">
                    {t(`logs.range.${r}` as const)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </CardHeader>
            <CardContent>
              <UsageChart range={range} />
            </CardContent>
          </Card>
          <Card className="min-w-0">
            <CardHeader>
              <CardTitle>{t('overview.u.breakdown')}</CardTitle>
              <CardDescription>{t(`overview.u.breakdownHint.${range}` as const)}</CardDescription>
            </CardHeader>
            <CardContent>
              <UsageBreakdown hours={OVERVIEW_RANGES[range].hours} />
            </CardContent>
          </Card>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
          <Card className="min-w-0">
            <CardHeader>
              <CardTitle>{t('overview.traffic')}</CardTitle>
              <CardDescription>{t('overview.trafficHint')}</CardDescription>
            </CardHeader>
            <CardContent>
              <TrafficChart logs={logs} />
            </CardContent>
          </Card>
          <Card className="min-w-0">
            <CardHeader>
              <CardTitle>{t('overview.topModels')}</CardTitle>
              <CardDescription>{t('overview.topModelsHint')}</CardDescription>
            </CardHeader>
            <CardContent>
              <TopModels logs={logs} />
            </CardContent>
          </Card>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
        <Card className="min-w-0">
          <CardHeader className="flex flex-row items-center gap-2">
            <span className="size-2 shrink-0 rounded-full bg-success text-success animate-pulse-ring" />
            <CardTitle>{t('overview.recent')}</CardTitle>
          </CardHeader>
          <CardContent>
            <ActivityFeed logs={logs} />
          </CardContent>
        </Card>
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>{t('overview.quickStart')}</CardTitle>
            <CardDescription>{t('overview.quickStartHint')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <CodeBlock code={snippet} />
            <Button asChild variant="outline" className="group w-full">
              <Link to="/playground">
                {t('overview.openPlayground')}
                <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
              </Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
