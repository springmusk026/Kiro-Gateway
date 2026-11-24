import { AlertTriangle, Ban, Clock, KeyRound, PauseCircle, ShieldAlert, Sparkles, Gauge } from 'lucide-react'
import type { ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { dateTime, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { Account } from '@/lib/types'
import { cn } from '@/lib/utils'
import { authLabel, health, isApiKeyAccount, isBanned, isExpired, isOverQuota, planTier, type Health } from './lib'

const dotTone: Record<Health, string> = {
  healthy: 'bg-success text-success animate-pulse-ring',
  expired: 'bg-success text-success',
  quota: 'bg-warning',
  noToken: 'bg-destructive',
  disabled: 'bg-muted-foreground/40',
  banned: 'bg-destructive',
  suspended: 'bg-destructive',
  sysDisabled: 'bg-warning',
}

export function HealthDot({ account, className }: { account: Account; className?: string }) {
  const { t } = useI18n()
  const h = health(account)
  return (
    <span
      role="img"
      aria-label={t(`accounts.health.${h}`)}
      title={t(`accounts.health.${h}`)}
      className={cn('inline-block size-2 shrink-0 rounded-full', dotTone[h], className)}
    />
  )
}

function Tip({ children, tip }: { children: ReactNode; tip?: ReactNode }) {
  if (!tip) return <>{children}</>
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex cursor-default">{children}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">{tip}</TooltipContent>
    </Tooltip>
  )
}

const tone = {
  danger: 'border-destructive/25 bg-destructive/10 text-destructive',
  warning: 'border-warning/30 bg-warning/12 text-[color-mix(in_oklab,var(--warning)_75%,var(--foreground))]',
  success: 'border-success/25 bg-success/10 text-success',
  info: 'border-info/25 bg-info/10 text-info',
  muted: 'border-border bg-muted text-muted-foreground',
  primary: 'border-primary/25 bg-primary/10 text-primary',
} as const
export type Tone = keyof typeof tone

export function Pill({ tone: tn, children, className }: { tone: Tone; children: ReactNode; className?: string }) {
  return (
    <Badge variant="outline" className={cn('h-5 gap-1 rounded-md px-1.5 text-[11px] font-medium', tone[tn], className)}>
      {children}
    </Badge>
  )
}

/** Status badges with explanations (ban reason, expiry, quota). */
export function StatusBadges({ account: a, compact }: { account: Account; compact?: boolean }) {
  const { t, lang } = useI18n()
  const out: ReactNode[] = []
  if (isBanned(a)) {
    const reason = (
      <div className="space-y-1">
        <p className="font-medium">{t('accounts.badge.bannedTip')}</p>
        {a.banReason && <p>{a.banReason}</p>}
        {a.banTime > 0 && <p className="opacity-80">{dateTime(a.banTime, lang)}</p>}
        <p className="opacity-80">{t('accounts.badge.reenableTip')}</p>
      </div>
    )
    out.push(
      <Tip key="ban" tip={reason}>
        <Pill tone={a.banStatus === 'DISABLED' ? 'warning' : 'danger'}>
          {a.banStatus === 'DISABLED' ? <ShieldAlert /> : <Ban />}
          {a.banStatus === 'SUSPENDED'
            ? t('accounts.badge.suspended')
            : a.banStatus === 'DISABLED'
              ? t('accounts.badge.sysDisabled')
              : t('accounts.badge.banned')}
        </Pill>
      </Tip>,
    )
  } else {
    if (!a.enabled)
      out.push(
        <Pill key="dis" tone="muted">
          <PauseCircle />
          {t('accounts.badge.disabled')}
        </Pill>,
      )
    if (!a.hasToken)
      out.push(
        <Tip key="tok" tip={t('accounts.badge.noTokenTip')}>
          <Pill tone="danger">
            <KeyRound />
            {t('accounts.badge.noToken')}
          </Pill>
        </Tip>,
      )
    else if (isExpired(a) && !isApiKeyAccount(a))
      out.push(
        <Tip key="exp" tip={t('accounts.badge.expiredTip', { when: timeAgo(a.expiresAt, lang) })}>
          <Pill tone="warning">
            <Clock />
            {t('accounts.badge.expired')}
          </Pill>
        </Tip>,
      )
    if (isOverQuota(a))
      out.push(
        <Tip key="q" tip={a.nextResetDate ? t('accounts.badge.quotaTip', { date: a.nextResetDate }) : undefined}>
          <Pill tone="warning">
            <Gauge />
            {t('accounts.badge.quota')}
          </Pill>
        </Tip>,
      )
    if (!out.length && !compact)
      out.push(
        <Pill key="ok" tone="success">
          {t('accounts.badge.healthy')}
        </Pill>,
      )
  }
  if (!out.length) return null
  return <div className="flex flex-wrap items-center gap-1">{out}</div>
}

const planTone = {
  power: 'border-chart-4/30 bg-chart-4/10 text-chart-4',
  proPlus: 'border-primary/30 bg-primary/10 text-primary',
  pro: 'border-info/30 bg-info/10 text-info',
  free: 'border-border bg-muted text-muted-foreground',
  other: 'border-border bg-muted text-muted-foreground',
} as const

export function PlanBadge({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const tier = planTier(a)
  if (!a.subscriptionType && !a.subscriptionTitle) return <span className="text-xs text-muted-foreground">–</span>
  const label = a.subscriptionTitle || (tier === 'other' ? a.subscriptionType || t('accounts.plan.unknown') : t(`accounts.plan.${tier}`))
  return (
    <Badge variant="outline" className={cn('h-5 max-w-40 rounded-md px-1.5 text-[11px]', planTone[tier])}>
      {tier === 'power' || tier === 'proPlus' ? <Sparkles /> : null}
      <span className="truncate">{label}</span>
    </Badge>
  )
}

export function TrialBadge({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  if (a.trialStatus !== 'ACTIVE' || a.trialUsageLimit <= 0) return null
  const pct = Math.round(Math.min(1, a.trialUsagePercent) * 100)
  return (
    <Tip
      tip={
        <span>
          {t('accounts.trial.tip', {
            used: a.trialUsageCurrent.toFixed(1),
            limit: a.trialUsageLimit.toFixed(0),
            pct,
          })}
          {a.trialExpiresAt > 0 && <> · {t('accounts.trial.expires', { when: timeAgo(a.trialExpiresAt, lang) })}</>}
        </span>
      }
    >
      <Pill tone="info">{t('accounts.trial.badge', { pct })}</Pill>
    </Tip>
  )
}

export function AuthBadge({ account }: { account: Account }) {
  const { t } = useI18n()
  return (
    <Badge variant="outline" className="h-5 rounded-md px-1.5 text-[11px] font-normal text-muted-foreground">
      {authLabel(account, t)}
    </Badge>
  )
}

/** Usage bar; `fraction` is 0..1. */
export function UsageBar({
  fraction,
  className,
  thin,
}: {
  fraction: number
  className?: string
  thin?: boolean
}) {
  const pct = Math.max(0, Math.min(100, fraction * 100))
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      className={cn('relative w-full overflow-hidden rounded-full bg-muted', thin ? 'h-1' : 'h-1.5', className)}
    >
      <div
        className={cn(
          'h-full rounded-full transition-[width] duration-700 ease-out',
          pct >= 90 ? 'bg-destructive' : pct >= 70 ? 'bg-warning' : 'bg-primary',
        )}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

export function UsageCell({ account: a }: { account: Account }) {
  const { t } = useI18n()
  if (a.usageLimit <= 0) {
    return <span className="text-xs text-muted-foreground">{a.lastRefresh ? t('accounts.usage.none') : t('accounts.usage.notLoaded')}</span>
  }
  const pct = a.usagePercent * 100
  return (
    <div className="w-full min-w-28 space-y-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="tabular font-medium">
          {a.usageCurrent.toFixed(1)}
          <span className="text-muted-foreground"> / {a.usageLimit.toFixed(0)}</span>
        </span>
        <span className={cn('tabular', pct >= 90 ? 'text-destructive' : 'text-muted-foreground')}>{pct.toFixed(1)}%</span>
      </div>
      <UsageBar fraction={a.usagePercent} />
      {a.nextResetDate && (
        <p className="text-[11px] text-muted-foreground">{t('accounts.usage.resets', { date: a.nextResetDate })}</p>
      )}
    </div>
  )
}

export function AttentionIcon() {
  return <AlertTriangle className="size-3.5 text-warning" />
}
