import { Power } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, maskEmail, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { Account } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AccountMenu } from './account-menu'
import { AuthBadge, HealthDot, PlanBadge, StatusBadges, TrialBadge, UsageCell } from './account-bits'
import { useAccountActions } from './actions'
import { isApiKeyAccount, isBanned, usePrivacyMode } from './lib'

interface ListProps {
  accounts: Account[]
  selected: ReadonlySet<string>
  onSelect: (id: string, on: boolean) => void
  onSelectAll: (on: boolean) => void
  highlight: ReadonlySet<string>
}

function Identity({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const [privacy] = usePrivacyMode()
  const act = useAccountActions()
  const email = a.email ? (privacy && a.email.includes('@') ? maskEmail(a.email) : a.email) : ''
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <HealthDot account={a} />
      <div className="min-w-0">
        <p className="truncate font-medium leading-tight">{act.name(a)}</p>
        <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          {a.nickname && email && <span className="truncate">{email}</span>}
          {a.nickname && email && <span className="opacity-50">·</span>}
          <span className="shrink-0 font-mono text-[11px]">{a.region || 'us-east-1'}</span>
          {a.weight >= 2 && (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="shrink-0 rounded bg-accent px-1 font-mono text-[10px] text-accent-foreground">
                  W{a.weight}
                </span>
              </TooltipTrigger>
              <TooltipContent>{t('accounts.weightTip', { n: a.weight })}</TooltipContent>
            </Tooltip>
          )}
        </p>
      </div>
    </div>
  )
}

function EnabledControl({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const act = useAccountActions()
  if (isBanned(a)) {
    return (
      <Button
        size="xs"
        variant="outline"
        onClick={(e) => {
          e.stopPropagation()
          act.reenable(a)
        }}
        disabled={act.busy.has(a.id)}
      >
        <Power /> {t('accounts.action.reenable')}
      </Button>
    )
  }
  return (
    <Switch
      checked={a.enabled}
      disabled={act.busy.has(a.id)}
      onClick={(e) => e.stopPropagation()}
      onCheckedChange={(v) => act.setEnabled(a, v)}
      aria-label={a.enabled ? t('common.disable') : t('common.enable')}
    />
  )
}

function Activity({ account: a, withLastUsed }: { account: Account; withLastUsed?: boolean }) {
  const { t, lang } = useI18n()
  return (
    <div className="space-y-0.5 text-xs tabular">
      <p>
        <span className="font-medium">{compact(a.requestCount)}</span>{' '}
        <span className="text-muted-foreground">{t('accounts.col.req')}</span>
        {a.errorCount > 0 && (
          <span className="ml-1.5 text-destructive">
            {compact(a.errorCount)} {t('accounts.col.err')}
          </span>
        )}
      </p>
      <p className="text-muted-foreground">
        {compact(a.totalTokens)} {t('accounts.col.tok')} · {a.totalCredits.toFixed(1)} {t('accounts.col.cr')}
      </p>
      {withLastUsed && (
        <p className="text-muted-foreground">
          {a.lastUsed ? t('accounts.lastUsed', { when: timeAgo(a.lastUsed, lang) }) : t('accounts.neverUsed')}
        </p>
      )}
    </div>
  )
}

function TokenCell({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  const expired = a.expiresAt > 0 && a.expiresAt < Date.now() / 1000
  return (
    <div className="space-y-0.5 text-xs">
      <p className={cn(expired && 'text-warning')}>
        {isApiKeyAccount(a) ? t('accounts.token.noExpiry') : a.expiresAt ? timeAgo(a.expiresAt, lang) : '–'}
      </p>
      <p className="text-muted-foreground">
        {a.lastUsed ? t('accounts.lastUsed', { when: timeAgo(a.lastUsed, lang) }) : t('accounts.neverUsed')}
      </p>
    </div>
  )
}

function TokenLine({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  // Expired tokens already get a badge (with the time in its tooltip).
  if (isApiKeyAccount(a) || !a.expiresAt || a.expiresAt < Date.now() / 1000 || isBanned(a)) return null
  return <p className="mt-1 text-[11px] text-muted-foreground">{t('accounts.token.expires', { when: timeAgo(a.expiresAt, lang) })}</p>
}

function useScrollToHighlight(highlight: ReadonlySet<string>) {
  const ref = useRef<HTMLElement | null>(null)
  useEffect(() => {
    if (highlight.size) ref.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [highlight])
  return ref
}

export function AccountsTable({ accounts, selected, onSelect, onSelectAll, highlight }: ListProps) {
  const { t } = useI18n()
  const act = useAccountActions()
  const hlRef = useScrollToHighlight(highlight)
  const selCount = accounts.filter((a) => selected.has(a.id)).length
  const allState = selCount === 0 ? false : selCount === accounts.length ? true : 'indeterminate'
  let hlAssigned = false
  return (
    <Card className="hidden gap-0 overflow-hidden p-0 lg:block">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/40 hover:bg-muted/40">
            <TableHead className="w-10 pl-4">
              <Checkbox
                checked={allState}
                onCheckedChange={(v) => onSelectAll(v === true)}
                aria-label={t('accounts.selectAll')}
              />
            </TableHead>
            <TableHead>{t('accounts.col.account')}</TableHead>
            <TableHead>{t('accounts.col.status')}</TableHead>
            <TableHead>{t('accounts.col.plan')}</TableHead>
            <TableHead className="w-44">{t('accounts.col.usage')}</TableHead>
            <TableHead>{t('accounts.col.activity')}</TableHead>
            <TableHead className="w-28 pr-3 text-right">{t('accounts.col.enabled')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className="stagger">
          {accounts.map((a) => {
            const hl = highlight.has(a.id)
            const attachRef = hl && !hlAssigned
            if (attachRef) hlAssigned = true
            return (
              <TableRow
                key={a.id}
                ref={attachRef ? (el) => void (hlRef.current = el) : undefined}
                data-state={selected.has(a.id) ? 'selected' : undefined}
                onClick={() => act.openDetail(a.id)}
                className={cn(
                  'cursor-pointer transition-colors',
                  !a.enabled && 'text-muted-foreground',
                  act.busy.has(a.id) && 'animate-pulse',
                  hl && 'bg-primary/8 shadow-[inset_3px_0_0_var(--primary)]',
                )}
              >
                <TableCell className="pl-4" onClick={(e) => e.stopPropagation()}>
                  <Checkbox
                    checked={selected.has(a.id)}
                    onCheckedChange={(v) => onSelect(a.id, v === true)}
                    aria-label={t('accounts.selectOne', { name: act.name(a) })}
                  />
                </TableCell>
                <TableCell className="max-w-60">
                  <Identity account={a} />
                </TableCell>
                <TableCell>
                  <StatusBadges account={a} />
                  <TokenLine account={a} />
                </TableCell>
                <TableCell>
                  <div className="flex flex-col items-start gap-1">
                    <div className="flex items-center gap-1">
                      <PlanBadge account={a} />
                      <TrialBadge account={a} />
                    </div>
                    <AuthBadge account={a} />
                  </div>
                </TableCell>
                <TableCell>
                  <UsageCell account={a} />
                </TableCell>
                <TableCell>
                  <Activity account={a} withLastUsed />
                </TableCell>
                <TableCell className="pr-3" onClick={(e) => e.stopPropagation()}>
                  <div className="flex items-center justify-end gap-1">
                    <EnabledControl account={a} />
                    <AccountMenu account={a} />
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </Card>
  )
}

export function AccountsCards({ accounts, selected, onSelect, highlight }: ListProps) {
  const { t } = useI18n()
  const act = useAccountActions()
  const hlRef = useScrollToHighlight(highlight)
  let hlAssigned = false
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:hidden stagger">
      {accounts.map((a) => {
        const hl = highlight.has(a.id)
        const attachRef = hl && !hlAssigned
        if (attachRef) hlAssigned = true
        return (
          <Card
            key={a.id}
            ref={attachRef ? (el: HTMLDivElement | null) => void (hlRef.current = el) : undefined}
            onClick={() => act.openDetail(a.id)}
            className={cn(
              'cursor-pointer gap-3 p-4 transition-shadow hover:shadow-md',
              selected.has(a.id) && 'ring-2 ring-primary/40',
              hl && 'ring-2 ring-primary',
              act.busy.has(a.id) && 'animate-pulse',
            )}
          >
            <div className="flex items-start gap-3">
              <div onClick={(e) => e.stopPropagation()} className="pt-0.5">
                <Checkbox
                  checked={selected.has(a.id)}
                  onCheckedChange={(v) => onSelect(a.id, v === true)}
                  aria-label={t('accounts.selectOne', { name: act.name(a) })}
                />
              </div>
              <div className="min-w-0 flex-1">
                <Identity account={a} />
              </div>
              <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                <EnabledControl account={a} />
                <AccountMenu account={a} />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-1">
              <StatusBadges account={a} compact />
              {(a.subscriptionType || a.subscriptionTitle) && <PlanBadge account={a} />}
              <TrialBadge account={a} />
              <AuthBadge account={a} />
            </div>
            <UsageCell account={a} />
            <div className="flex items-end justify-between gap-3">
              <Activity account={a} />
              <div className="text-right">
                <TokenCell account={a} />
              </div>
            </div>
          </Card>
        )
      })}
    </div>
  )
}
