import {
  AlertCircle,
  Boxes,
  ClipboardCopy,
  CloudDownload,
  FlaskConical,
  Loader2,
  Power,
  PowerOff,
  RefreshCw,
  RotateCcw,
  Save,
  Trash2,
  Wand2,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { CopyButton } from '@/components/app/copy-button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { compact, dateTime, maskEmail, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import {
  accountApi,
  useAccountModels,
  useRefreshAccountModels,
  useSetOverage,
  useSyncOverage,
  useUpdateAccount,
} from '@/lib/queries.accounts'
import type { Account, AccountPatch } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AuthBadge, HealthDot, PlanBadge, StatusBadges, UsageBar } from './account-bits'
import { useAccountActions, type DetailTab } from './actions'
import { errMsg, isApiKeyAccount, isBanned, MACHINE_ID_RE, PROXY_RE, usePrivacyMode } from './lib'

function Section({ title, description, children, action }: { title: string; description?: string; children: ReactNode; action?: ReactNode }) {
  return (
    <section className="rounded-xl border bg-card p-4">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">{title}</h3>
          {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function Field({ label, children, mono, copy }: { label: string; children: ReactNode; mono?: boolean; copy?: string }) {
  return (
    <div className="flex min-h-8 items-center justify-between gap-4 py-1 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className={cn('flex min-w-0 items-center gap-1 text-right', mono && 'font-mono text-xs')}>
        <span className="truncate">{children}</span>
        {copy && <CopyButton value={copy} className="size-6" />}
      </dd>
    </div>
  )
}

export function AccountDetailSheet({
  account,
  tab,
  onTabChange,
  onClose,
}: {
  account: Account | undefined
  tab: DetailTab
  onTabChange: (t: DetailTab) => void
  onClose: () => void
}) {
  const { t } = useI18n()
  const act = useAccountActions()
  return (
    <Sheet open={!!account} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 p-0 sm:max-w-xl">
        {account && (
          <>
            <SheetHeader className="border-b p-5 pr-12">
              <div className="flex items-center gap-2.5">
                <HealthDot account={account} className="size-2.5" />
                <SheetTitle className="truncate text-lg">{act.name(account)}</SheetTitle>
              </div>
              <SheetDescription className="sr-only">{t('accounts.detail.description')}</SheetDescription>
              <div className="flex flex-wrap items-center gap-1">
                <StatusBadges account={account} />
                <PlanBadge account={account} />
                <AuthBadge account={account} />
              </div>
              <HeaderActions account={account} onClose={onClose} />
            </SheetHeader>
            <Tabs value={tab} onValueChange={(v) => onTabChange(v as DetailTab)} className="min-h-0 flex-1 gap-0">
              <div className="border-b px-5 py-2">
                <TabsList>
                  <TabsTrigger value="overview">{t('accounts.detail.tabOverview')}</TabsTrigger>
                  <TabsTrigger value="settings">{t('accounts.detail.tabSettings')}</TabsTrigger>
                  <TabsTrigger value="models">{t('accounts.detail.tabModels')}</TabsTrigger>
                </TabsList>
              </div>
              <ScrollArea className="min-h-0 flex-1">
                <div className="space-y-4 p-5">
                  <TabsContent value="overview" className="mt-0 space-y-4 animate-rise">
                    <OverviewTab account={account} />
                  </TabsContent>
                  <TabsContent value="settings" className="mt-0 space-y-4 animate-rise">
                    <SettingsForm key={account.id} account={account} />
                    <OverageSection account={account} />
                  </TabsContent>
                  <TabsContent value="models" className="mt-0 space-y-4 animate-rise">
                    <ModelsTab account={account} />
                  </TabsContent>
                </div>
              </ScrollArea>
            </Tabs>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}

function HeaderActions({ account: a, onClose }: { account: Account; onClose: () => void }) {
  const { t } = useI18n()
  const act = useAccountActions()
  const busy = act.busy.has(a.id)
  return (
    <div className="mt-2 flex flex-wrap gap-2">
      <Button size="sm" variant="outline" onClick={() => act.refresh(a)} disabled={busy}>
        {busy ? <Loader2 className="animate-spin" /> : <RefreshCw />} {t('accounts.action.refresh')}
      </Button>
      <Button size="sm" variant="outline" onClick={() => act.openTest(a.id)}>
        <FlaskConical /> {t('accounts.action.test')}
      </Button>
      {isBanned(a) ? (
        <Button size="sm" variant="outline" onClick={() => act.reenable(a)} disabled={busy}>
          <Power /> {t('accounts.action.reenable')}
        </Button>
      ) : (
        <Button size="sm" variant="outline" onClick={() => act.setEnabled(a, !a.enabled)} disabled={busy}>
          {a.enabled ? <PowerOff /> : <Power />} {a.enabled ? t('common.disable') : t('common.enable')}
        </Button>
      )}
      <Button size="sm" variant="outline" onClick={() => act.copyCredentials(a)} disabled={busy}>
        <ClipboardCopy /> {t('accounts.action.copyCredentialsShort')}
      </Button>
      <Button
        size="sm"
        variant="ghost"
        className="text-destructive hover:bg-destructive/10 hover:text-destructive"
        onClick={async () => {
          if (await act.remove(a)) onClose()
        }}
        disabled={busy}
      >
        <Trash2 /> {t('common.delete')}
      </Button>
    </div>
  )
}

function OverviewTab({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  const [privacy] = usePrivacyMode()
  const email = a.email ? (privacy && a.email.includes('@') ? maskEmail(a.email) : a.email) : '–'
  return (
    <>
      {isBanned(a) && (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t('accounts.detail.bannedTitle', { status: a.banStatus })}</AlertTitle>
          <AlertDescription>
            {a.banReason || t('accounts.detail.bannedNoReason')}
            {a.banTime > 0 && <span className="block opacity-80">{dateTime(a.banTime, lang)}</span>}
          </AlertDescription>
        </Alert>
      )}
      <Section title={t('accounts.detail.identity')}>
        <dl className="divide-y">
          <Field label={t('accounts.detail.email')}>{email}</Field>
          {a.nickname && <Field label={t('accounts.detail.nickname')}>{a.nickname}</Field>}
          <Field label={t('accounts.detail.userId')} mono copy={a.userId || undefined}>
            {a.userId || '–'}
          </Field>
          <Field label={t('accounts.detail.accountId')} mono copy={a.id}>
            {a.id}
          </Field>
          <Field label={t('accounts.detail.authMethod')}>
            <AuthBadge account={a} />
          </Field>
          <Field label={t('accounts.detail.region')} mono>
            {a.region || 'us-east-1'}
          </Field>
        </dl>
      </Section>

      <Section
        title={t('accounts.detail.subscription')}
        description={a.lastRefresh ? t('accounts.detail.lastRefresh', { when: timeAgo(a.lastRefresh, lang) }) : t('accounts.detail.neverRefreshed')}
      >
        <dl className="divide-y">
          <Field label={t('accounts.detail.plan')}>
            <PlanBadge account={a} />
          </Field>
          {a.daysRemaining > 0 && <Field label={t('accounts.detail.daysRemaining')}>{a.daysRemaining}</Field>}
        </dl>
        {a.usageLimit > 0 && (
          <div className="mt-3 space-y-1.5">
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">{t('accounts.detail.mainQuota')}</span>
              <span className="tabular">
                {a.usageCurrent.toFixed(1)} / {a.usageLimit.toFixed(0)} · {(a.usagePercent * 100).toFixed(1)}%
              </span>
            </div>
            <UsageBar fraction={a.usagePercent} />
            {a.nextResetDate && <p className="text-xs text-muted-foreground">{t('accounts.usage.resets', { date: a.nextResetDate })}</p>}
          </div>
        )}
        {a.trialUsageLimit > 0 && (
          <div className="mt-4 space-y-1.5">
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">
                {t('accounts.detail.trialQuota')} · {a.trialStatus || '–'}
              </span>
              <span className="tabular">
                {a.trialUsageCurrent.toFixed(1)} / {a.trialUsageLimit.toFixed(0)} · {(a.trialUsagePercent * 100).toFixed(1)}%
              </span>
            </div>
            <UsageBar fraction={a.trialUsagePercent} />
            {a.trialExpiresAt > 0 && (
              <p className="text-xs text-muted-foreground">
                {t('accounts.trial.expires', { when: timeAgo(a.trialExpiresAt, lang) })} · {dateTime(a.trialExpiresAt, lang)}
              </p>
            )}
          </div>
        )}
      </Section>

      <Section title={t('accounts.detail.stats')} description={!a.enabled ? t('accounts.detail.statsDisabled') : undefined}>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {[
            [t('accounts.detail.requests'), compact(a.requestCount)],
            [t('accounts.detail.errors'), compact(a.errorCount)],
            [t('accounts.detail.tokens'), compact(a.totalTokens)],
            [t('accounts.detail.credits'), a.totalCredits.toFixed(2)],
          ].map(([k, v], i) => (
            <div key={k} className="rounded-lg bg-muted/50 p-3">
              <p className="text-[11px] text-muted-foreground">{k}</p>
              <p className={cn('mt-0.5 text-lg font-semibold tabular', i === 1 && a.errorCount > 0 && 'text-destructive')}>{v}</p>
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          {a.lastUsed ? t('accounts.lastUsed', { when: timeAgo(a.lastUsed, lang) }) : t('accounts.neverUsed')}
        </p>
      </Section>

      <Section title={t('accounts.detail.technical')}>
        <dl className="divide-y">
          <Field label={t('accounts.detail.tokenExpiry')}>
            {isApiKeyAccount(a)
              ? t('accounts.token.noExpiry')
              : a.expiresAt
                ? `${timeAgo(a.expiresAt, lang)} · ${dateTime(a.expiresAt, lang)}`
                : '–'}
          </Field>
          <Field label={t('accounts.detail.hasToken')}>{a.hasToken ? t('common.yes') : t('common.no')}</Field>
          <Field label={t('accounts.detail.machineId')} mono copy={a.machineId || undefined}>
            {a.machineId || '–'}
          </Field>
          <Field label={t('accounts.detail.weight')}>{a.weight}</Field>
          <Field label={t('accounts.detail.proxy')} mono>
            {a.proxyURL || t('accounts.detail.proxyGlobal')}
          </Field>
        </dl>
      </Section>
    </>
  )
}

function SettingsForm({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const update = useUpdateAccount()
  const [nickname, setNickname] = useState(a.nickname)
  const [weight, setWeight] = useState(a.weight)
  const [proxyURL, setProxyURL] = useState(a.proxyURL)
  const [machineId, setMachineId] = useState(a.machineId)
  const [genBusy, setGenBusy] = useState(false)

  const patch = useMemo(() => {
    const p: AccountPatch = {}
    if (nickname.trim() !== a.nickname) p.nickname = nickname.trim()
    if (weight !== a.weight) p.weight = weight
    if (proxyURL.trim() !== a.proxyURL) p.proxyURL = proxyURL.trim()
    if (machineId.trim() !== a.machineId) p.machineId = machineId.trim()
    return p
  }, [nickname, weight, proxyURL, machineId, a])
  const dirty = Object.keys(patch).length > 0
  const proxyErr = proxyURL.trim() && !PROXY_RE.test(proxyURL.trim()) ? t('accounts.detail.proxyError') : ''
  const midErr =
    patch.machineId !== undefined && patch.machineId !== '' && !MACHINE_ID_RE.test(patch.machineId)
      ? t('accounts.detail.machineIdError')
      : ''

  const save = async () => {
    if (proxyErr || midErr || !dirty) return
    try {
      await update.mutateAsync({ id: a.id, patch })
      toast.success(t('accounts.detail.saved'))
    } catch (e) {
      toast.error(t('accounts.detail.saveFailed', { error: errMsg(e) }))
    }
  }
  const reset = () => {
    setNickname(a.nickname)
    setWeight(a.weight)
    setProxyURL(a.proxyURL)
    setMachineId(a.machineId)
  }
  const gen = async () => {
    setGenBusy(true)
    try {
      setMachineId(await accountApi.generateMachineId())
    } catch (e) {
      toast.error(errMsg(e))
    } finally {
      setGenBusy(false)
    }
  }

  return (
    <Section title={t('accounts.detail.settings')} description={t('accounts.detail.settingsHint')}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          save()
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="acc-nick">{t('accounts.detail.nickname')}</Label>
          <Input id="acc-nick" value={nickname} onChange={(e) => setNickname(e.target.value)} placeholder={t('accounts.detail.nicknamePlaceholder')} />
        </div>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Label htmlFor="acc-weight">{t('accounts.detail.weight')}</Label>
            <Input
              id="acc-weight"
              type="number"
              min={0}
              max={10}
              value={weight}
              onChange={(e) => setWeight(Math.max(0, Math.min(10, parseInt(e.target.value, 10) || 0)))}
              className="h-8 w-16 text-right tabular"
            />
          </div>
          <Slider min={0} max={10} step={1} value={[weight]} onValueChange={(v) => setWeight(v[0] ?? 0)} aria-label={t('accounts.detail.weight')} />
          <p className="text-xs text-muted-foreground">{t('accounts.detail.weightHint')}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="acc-proxy">{t('accounts.detail.proxy')}</Label>
          <Input
            id="acc-proxy"
            value={proxyURL}
            onChange={(e) => setProxyURL(e.target.value)}
            placeholder="socks5://host:port"
            className="font-mono text-xs"
            aria-invalid={!!proxyErr}
          />
          <p className={cn('text-xs', proxyErr ? 'text-destructive' : 'text-muted-foreground')}>{proxyErr || t('accounts.detail.proxyHint')}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="acc-mid">{t('accounts.detail.machineId')}</Label>
          <div className="flex gap-2">
            <Input
              id="acc-mid"
              value={machineId}
              onChange={(e) => setMachineId(e.target.value)}
              className="font-mono text-xs"
              aria-invalid={!!midErr}
            />
            <Button type="button" variant="outline" onClick={gen} disabled={genBusy}>
              {genBusy ? <Loader2 className="animate-spin" /> : <Wand2 />} {t('accounts.detail.generate')}
            </Button>
          </div>
          <p className={cn('text-xs', midErr ? 'text-destructive' : 'text-muted-foreground')}>{midErr || t('accounts.detail.machineIdHint')}</p>
        </div>
        <div className="flex justify-end gap-2 border-t pt-4">
          <Button type="button" variant="ghost" onClick={reset} disabled={!dirty || update.isPending}>
            {t('accounts.detail.discard')}
          </Button>
          <Button type="submit" disabled={!dirty || !!proxyErr || !!midErr || update.isPending}>
            {update.isPending ? <Loader2 className="animate-spin" /> : <Save />} {t('common.save')}
          </Button>
        </div>
      </form>
    </Section>
  )
}

function OverageSection({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  const setOv = useSetOverage()
  const sync = useSyncOverage()
  const [error, setError] = useState('')
  const [pending, setPending] = useState<boolean | null>(null)
  useEffect(() => setError(''), [a.id])
  const notCapable = !!a.overageCapability && a.overageCapability !== 'OVERAGE_CAPABLE'
  const checked = pending ?? a.overageStatus === 'ENABLED'
  const statusLabel =
    a.overageStatus === 'ENABLED'
      ? t('accounts.overage.enabled')
      : a.overageStatus === 'DISABLED'
        ? t('accounts.overage.disabled')
        : t('accounts.overage.unknown')

  const toggle = async (v: boolean) => {
    setPending(v)
    setError('')
    try {
      const r = await setOv.mutateAsync({ id: a.id, enabled: v })
      toast.success(t('accounts.overage.switched', { status: r.overageStatus }))
    } catch (e) {
      setError(errMsg(e))
      toast.error(t('accounts.overage.switchFailed'))
    } finally {
      setPending(null)
    }
  }
  const doSync = async () => {
    setError('')
    try {
      await sync.mutateAsync(a.id)
      toast.success(t('accounts.overage.synced'))
    } catch (e) {
      setError(errMsg(e))
    }
  }

  return (
    <Section
      title={t('accounts.overage.title')}
      description={t('accounts.overage.hint')}
      action={
        <Button size="sm" variant="outline" onClick={doSync} disabled={sync.isPending}>
          {sync.isPending ? <Loader2 className="animate-spin" /> : <CloudDownload />} {t('accounts.overage.sync')}
        </Button>
      }
    >
      <div className="flex items-center justify-between gap-4 rounded-lg border bg-muted/30 p-3">
        <div className="min-w-0">
          <Label htmlFor="acc-overage" className="text-sm">
            {setOv.isPending ? t('accounts.overage.switching') : statusLabel}
          </Label>
          {notCapable && <p className="mt-0.5 text-xs text-muted-foreground">{t('accounts.overage.notCapable')}</p>}
        </div>
        <div className="flex items-center gap-2">
          {setOv.isPending && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Switch id="acc-overage" checked={checked} disabled={notCapable || setOv.isPending} onCheckedChange={toggle} />
        </div>
      </div>
      {error && (
        <Alert variant="destructive" className="mt-3">
          <AlertCircle />
          <AlertTitle>{t('accounts.overage.upstreamError')}</AlertTitle>
          <AlertDescription className="break-words">{error}</AlertDescription>
        </Alert>
      )}
      <dl className="mt-3 divide-y">
        <Field label={t('accounts.overage.capability')} mono>
          {a.overageCapability || '–'}
        </Field>
        <Field label={t('accounts.overage.cap')}>${a.overageCap.toFixed(2)}</Field>
        <Field label={t('accounts.overage.rate')}>${a.overageRate.toFixed(4)}</Field>
        <Field label={t('accounts.overage.current')}>${a.currentOverages ? a.currentOverages.toFixed(4) : '0'}</Field>
        <Field label={t('accounts.overage.checkedAt')}>{a.overageCheckedAt ? dateTime(a.overageCheckedAt, lang) : t('common.never')}</Field>
      </dl>
    </Section>
  )
}

function ModelsTab({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const q = useAccountModels(a.id, true)
  const refresh = useRefreshAccountModels()
  const models = useMemo(
    () =>
      (q.data ?? []).slice().sort((x, y) => {
        if (x.modelId === 'auto') return -1
        if (y.modelId === 'auto') return 1
        return (x.rateMultiplier || 1) - (y.rateMultiplier || 1) || x.modelId.localeCompare(y.modelId)
      }),
    [q.data],
  )
  return (
    <Section
      title={t('accounts.models.title')}
      description={t('accounts.models.hint')}
      action={
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" onClick={() => q.refetch()} disabled={q.isFetching} aria-label={t('accounts.models.reload')}>
            <RefreshCw className={cn(q.isFetching && 'animate-spin')} />
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={refresh.isPending}
            onClick={async () => {
              try {
                const r = await refresh.mutateAsync(a.id)
                toast.success(t('accounts.toast.modelsRefreshed', { n: r.count }))
              } catch (e) {
                toast.error(errMsg(e))
              }
            }}
          >
            {refresh.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />} {t('accounts.models.refreshCache')}
          </Button>
        </div>
      }
    >
      {q.isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : q.isError ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t('accounts.models.loadFailed')}</AlertTitle>
          <AlertDescription>
            {errMsg(q.error)}
            <Button size="sm" variant="outline" className="mt-2" onClick={() => q.refetch()}>
              {t('common.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : models.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center text-sm text-muted-foreground">
          <Boxes className="size-5" />
          {t('accounts.models.empty')}
        </div>
      ) : (
        <ul className="divide-y stagger">
          {models.map((m) => (
            <li key={m.modelId} className="flex items-start justify-between gap-3 py-2.5">
              <div className="min-w-0">
                <p className="truncate font-mono text-[13px]">{m.modelId}</p>
                {(m.modelName || m.description) && (
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{m.description || m.modelName}</p>
                )}
                {m.tokenLimits && (
                  <p className="mt-0.5 text-[11px] text-muted-foreground tabular">
                    {t('accounts.models.limits', {
                      input: compact(m.tokenLimits.maxInputTokens),
                      output: compact(m.tokenLimits.maxOutputTokens),
                    })}
                  </p>
                )}
              </div>
              <Badge variant="outline" className="shrink-0 tabular">
                {t('accounts.models.multiplier', { n: m.rateMultiplier || 1 })}
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}
