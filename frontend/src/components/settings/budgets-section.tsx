import { BellRing, Gauge, Loader2, Send, Wallet } from 'lucide-react'
import { useMemo } from 'react'
import { toast } from 'sonner'
import { CostAlertList } from '@/components/app/cost-alerts'
import { UsageBar } from '@/components/accounts/account-bits'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useI18n } from '@/lib/i18n'
import { useBudgets, useSaveBudgets, useTestAlertWebhook } from '@/lib/queries.cost'
import { cn } from '@/lib/utils'
import { SectionCard, SettingRow, runSave, useDraft } from './section-card'

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2))
const NUM = /^\d*\.?\d+$/

/** Settings → Routing: the usage threshold for usage-aware routing. */
export function UsageRoutingCard() {
  const { t } = useI18n()
  const q = useBudgets(false)
  const save = useSaveBudgets()
  const server = useMemo(
    () => (q.data ? { pct: String(Math.round(q.data.usageRoutingThreshold * 1000) / 10) } : undefined),
    [q.data],
  )
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  const pct = Number(draft?.pct)
  const invalid = !!draft && (!NUM.test(draft.pct.trim()) || pct < 1 || pct > 100)
  return (
    <SectionCard
      icon={Gauge}
      title={t('cost.routing')}
      description={t('cost.routingDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={invalid}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync({ usageRoutingThreshold: pct / 100 }), t('settings.saved')))) commit()
      }}
    >
      {draft && (
        <div className="space-y-3">
          <SettingRow id="usage-threshold" label={t('cost.threshold')} hint={t('cost.thresholdHint')}>
            <div className="flex items-center gap-2">
              <Input
                id="usage-threshold"
                inputMode="decimal"
                className="w-24 text-right tabular"
                value={draft.pct}
                aria-invalid={invalid}
                disabled={q.data?.thresholdFromEnv}
                onChange={(e) => setDraft(() => ({ pct: e.target.value }))}
              />
              <span className="text-sm text-muted-foreground">%</span>
            </div>
          </SettingRow>
          {invalid && <p className="text-xs text-destructive">{t('cost.thresholdInvalid')}</p>}
          {q.data?.thresholdFromEnv && <p className="text-xs text-warning">{t('cost.thresholdEnv')}</p>}
        </div>
      )}
    </SectionCard>
  )
}

type BudgetForm = { limit: string; tz: string; webhook: string }

/** Settings → Budgets & alerts: the global daily cap. */
export function BudgetsCard() {
  const { t, lang } = useI18n()
  const q = useBudgets()
  const save = useSaveBudgets()
  const server = useMemo<BudgetForm | undefined>(
    () =>
      q.data
        ? {
            limit: q.data.dailyCreditLimit ? String(q.data.dailyCreditLimit) : '',
            tz: q.data.budgetTimeZone,
            webhook: q.data.alertWebhookURL,
          }
        : undefined,
    [q.data],
  )
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  const limitErr = !!draft && draft.limit.trim() !== '' && !NUM.test(draft.limit.trim())
  const today = q.data?.today
  const limit = q.data?.dailyCreditLimit ?? 0
  const resets = today ? new Date(today.resetsAt).toLocaleString(lang, { dateStyle: 'medium', timeStyle: 'short' }) : ''

  return (
    <SectionCard
      icon={Wallet}
      title={t('cost.budgets')}
      description={t('cost.budgetsDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={limitErr}
      onReset={reset}
      onSave={async () => {
        if (!draft) return
        const patch = { dailyCreditLimit: draft.limit.trim() ? Number(draft.limit) : 0, budgetTimeZone: draft.tz.trim() }
        if (await runSave(() => save.mutateAsync(patch), t('settings.saved'))) commit()
      }}
      footerExtra={
        today && (
          <span className="text-muted-foreground">
            {limit > 0
              ? t('cost.todayUsageOf', { used: fmt(today.globalCredits), limit: fmt(limit) })
              : t('cost.todayUsage', { used: fmt(today.globalCredits) })}
            {' · '}
            {t('cost.resets', { when: resets })}
          </span>
        )
      }
    >
      {draft && (
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="budget-limit">{t('cost.dailyLimit')}</Label>
              <Input
                id="budget-limit"
                inputMode="decimal"
                className="tabular"
                placeholder={t('cost.unlimited')}
                value={draft.limit}
                aria-invalid={limitErr}
                onChange={(e) => setDraft((d) => ({ ...d, limit: e.target.value }))}
              />
              {limitErr && <p className="text-xs text-destructive">{t('cost.limitInvalid')}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="budget-tz">{t('cost.timeZone')}</Label>
              <Input
                id="budget-tz"
                className="font-mono text-[13px]"
                placeholder="UTC"
                value={draft.tz}
                onChange={(e) => setDraft((d) => ({ ...d, tz: e.target.value }))}
              />
              <p className="text-xs text-muted-foreground">{t('cost.timeZoneHint')}</p>
            </div>
          </div>
          {limit > 0 && today && <UsageBar fraction={today.globalCredits / limit} />}
        </div>
      )}
    </SectionCard>
  )
}

/** Settings → Budgets & alerts: webhook and the alerts active right now. */
export function AlertsCard() {
  const { t } = useI18n()
  const q = useBudgets()
  const save = useSaveBudgets()
  const test = useTestAlertWebhook()
  const server = useMemo(() => (q.data ? { webhook: q.data.alertWebhookURL } : undefined), [q.data])
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  const urlErr = !!draft && draft.webhook.trim() !== '' && !/^https?:\/\/\S+$/i.test(draft.webhook.trim())
  const alerts = q.data?.alerts ?? []

  return (
    <SectionCard
      icon={BellRing}
      title={t('cost.alerts')}
      description={t('cost.alertsDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={urlErr}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync({ alertWebhookURL: draft.webhook.trim() }), t('settings.saved')))) commit()
      }}
      footerExtra={
        !!q.data?.alertWebhookURL && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={test.isPending}
            onClick={() =>
              test.mutate(undefined, {
                onSuccess: () => toast.success(t('cost.testSent')),
                onError: (e) => toast.error(e.message),
              })
            }
          >
            {test.isPending ? <Loader2 className="animate-spin" /> : <Send />}
            {t('cost.testWebhook')}
          </Button>
        )
      }
    >
      {draft && (
        <div className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="alert-webhook">{t('cost.webhook')}</Label>
            <Input
              id="alert-webhook"
              type="url"
              className="font-mono text-[13px]"
              placeholder="https://hooks.slack.com/services/…"
              value={draft.webhook}
              aria-invalid={urlErr}
              onChange={(e) => setDraft(() => ({ webhook: e.target.value }))}
            />
            <p className={cn('text-xs', urlErr ? 'text-destructive' : 'text-muted-foreground')}>
              {t(urlErr ? 'cost.webhookInvalid' : 'cost.webhookHint')}
            </p>
          </div>
          <div className="space-y-2">
            <p className="text-sm font-medium">{t('cost.activeAlerts')}</p>
            {alerts.length ? (
              <CostAlertList alerts={alerts} />
            ) : (
              <p className="text-sm text-muted-foreground">{t('cost.noAlerts')}</p>
            )}
          </div>
        </div>
      )}
    </SectionCard>
  )
}
