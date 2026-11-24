import { Link } from '@tanstack/react-router'
import { AlertTriangle, OctagonAlert } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useI18n } from '@/lib/i18n'
import { useAlerts, type CostAlert } from '@/lib/queries.cost'
import { cn } from '@/lib/utils'

/** One line per alert: level icon plus the alert text. */
export function CostAlertList({ alerts, className }: { alerts: CostAlert[]; className?: string }) {
  return (
    <ul className={cn('space-y-1', className)}>
      {alerts.map((a) => (
        <li key={`${a.kind}|${a.subject ?? ''}`} className="flex items-start gap-2 text-sm">
          {a.level === 'critical' ? (
            <OctagonAlert className="mt-0.5 size-4 shrink-0 text-destructive" aria-label="critical" />
          ) : (
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" aria-label="warning" />
          )}
          <span className="min-w-0">{a.text.replace(/^Kiro Gateway: /, '')}</span>
        </li>
      ))}
    </ul>
  )
}

/**
 * Banner for active budget and usage alerts (Overview, API & Keys). Renders
 * nothing while there are none.
 */
export function CostAlertsBanner({ className }: { className?: string }) {
  const { t } = useI18n()
  const { data } = useAlerts()
  if (!data?.length) return null
  const critical = data.filter((a) => a.level === 'critical').length
  return (
    <Alert
      className={cn(
        'animate-rise',
        critical ? 'border-destructive/40 bg-destructive/[0.06]' : 'border-warning/40 bg-warning/10',
        className,
      )}
    >
      {critical ? <OctagonAlert className="text-destructive" /> : <AlertTriangle className="text-warning" />}
      <AlertTitle className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span>{t('cost.bannerTitle')}</span>
        <span className="text-xs font-normal text-muted-foreground">
          {critical
            ? t('cost.bannerCritical', { n: critical })
            : t('cost.bannerWarning', { n: data.length })}
        </span>
        <Link to="/settings" hash="budgets" className="ml-auto text-xs font-medium text-primary underline-offset-4 hover:underline">
          {t('cost.manage')}
        </Link>
      </AlertTitle>
      <AlertDescription className="text-foreground/85">
        <CostAlertList alerts={data} className="mt-1" />
      </AlertDescription>
    </Alert>
  )
}
