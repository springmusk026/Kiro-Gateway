import { createFileRoute, Link } from '@tanstack/react-router'
import { Activity, ArrowRight, CheckCircle2, Coins, ShieldAlert, Users } from 'lucide-react'
import { useMemo } from 'react'
import { CodeBlock } from '@/components/app/code-block'
import { StatCard } from '@/components/app/stat-card'
import { ActivityFeed } from '@/components/overview/activity-feed'
import { TopModels } from '@/components/overview/top-models'
import { TrafficChart } from '@/components/overview/traffic-chart'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { compact, duration } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useLogs, useModels, useStatus } from '@/lib/queries'

export const Route = createFileRoute('/_app/')({ component: OverviewPage })

function greetingKey() {
  const h = new Date().getHours()
  return h < 12 ? 'overview.greetingMorning' : h < 18 ? 'overview.greetingAfternoon' : 'overview.greetingEvening'
}

function OverviewPage() {
  const { t } = useI18n()
  const { data: status } = useStatus()
  const { data: logs = [] } = useLogs(10_000)
  const { data: catalog } = useModels()

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

      {!status ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[118px] rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4 stagger">
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

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>{t('overview.traffic')}</CardTitle>
            <CardDescription>{t('overview.trafficHint')}</CardDescription>
          </CardHeader>
          <CardContent>
            <TrafficChart logs={logs} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('overview.topModels')}</CardTitle>
            <CardDescription>{t('overview.topModelsHint')}</CardDescription>
          </CardHeader>
          <CardContent>
            <TopModels logs={logs} />
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader className="flex-row items-center gap-2">
            <span className="size-2 rounded-full bg-success text-success animate-pulse-ring" />
            <CardTitle>{t('overview.recent')}</CardTitle>
          </CardHeader>
          <CardContent>
            <ActivityFeed logs={logs} />
          </CardContent>
        </Card>
        <Card>
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
