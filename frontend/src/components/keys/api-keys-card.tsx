import { Link } from '@tanstack/react-router'
import { KeyRound, MoreHorizontal, Pencil, Plus, RefreshCw, RotateCcw, ShieldAlert, ShieldCheck, Trash2 } from 'lucide-react'
import { TablePagination } from '@/components/app/table-pagination'
import { usePagination } from '@/hooks/use-pagination'
import { useState } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { EmptyState } from '@/components/app/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, dateTime, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useApiKeys, useDeleteApiKey, useResetApiKeyUsage, useSettings, useUpdateApiKey } from '@/lib/queries.settings'
import type { ApiKeyView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { KeyDialog, RevealDialog } from './key-dialogs'
import { RestrictionBadges } from './key-restriction-badges'

function UsageMeter({ label, used, limit, format }: { label: string; used: number; limit?: number; format: (n: number) => string }) {
  const { t } = useI18n()
  const pct = limit && limit > 0 ? Math.min(100, (100 * used) / limit) : null
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular">
          <span className="font-medium">{format(used)}</span>
          <span className="text-muted-foreground"> / {limit && limit > 0 ? format(limit) : t('keys.unlimited')}</span>
        </span>
      </div>
      <div className={cn('mt-1.5 h-1.5 overflow-hidden rounded-full', pct != null && 'bg-muted')} aria-hidden={pct == null}>
        {pct != null && (
          <div
            role="progressbar"
            aria-valuenow={Math.round(pct)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={label}
            className={cn(
              'h-full rounded-full transition-[width] duration-500',
              pct >= 95 ? 'bg-destructive' : pct >= 80 ? 'bg-warning' : 'bg-primary',
            )}
            style={{ width: `${pct}%` }}
          />
        )}
      </div>
    </div>
  )
}

function KeyRow({ k, onEdit, onRegenerated }: { k: ApiKeyView; onEdit: () => void; onRegenerated: (plaintext: string) => void }) {
  const { t, lang } = useI18n()
  const confirm = useConfirm()
  const update = useUpdateApiKey()
  const remove = useDeleteApiKey()
  const reset = useResetApiKeyUsage()
  const name = k.name || t('keys.unnamed')
  const exhausted =
    (!!k.tokenLimit && k.tokenLimit > 0 && k.tokensUsed >= k.tokenLimit) ||
    (!!k.creditLimit && k.creditLimit > 0 && k.creditsUsed >= k.creditLimit)

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn()
      toast.success(ok)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <li className={cn('grid gap-4 px-5 py-4 sm:px-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1.4fr)_auto] lg:items-center', !k.enabled && 'bg-muted/30')}>
      <div className="flex min-w-0 items-start gap-3">
        <div
          className={cn(
            'mt-0.5 grid size-9 shrink-0 place-items-center rounded-lg',
            k.enabled ? 'bg-accent text-accent-foreground' : 'bg-muted text-muted-foreground',
          )}
        >
          <KeyRound className="size-4" />
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={cn('truncate font-medium', !k.name && 'italic text-muted-foreground')}>{name}</span>
            {k.migrated && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="secondary">{t('keys.migrated')}</Badge>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">{t('keys.migratedHint')}</TooltipContent>
              </Tooltip>
            )}
            {!k.enabled && <Badge variant="outline">{t('common.disabled')}</Badge>}
            {exhausted && (
              <Badge variant="outline" className="border-destructive/40 text-destructive">
                {t('keys.limitReached')}
              </Badge>
            )}
            <RestrictionBadges k={k} />
          </div>
          <code className="mt-0.5 block truncate font-mono text-[12.5px] text-muted-foreground">{k.keyHint}</code>
          <p className="mt-1 text-xs text-muted-foreground">
            <Tooltip>
              <TooltipTrigger className="cursor-default">{t('keys.createdAgo', { when: timeAgo(k.createdAt, lang) })}</TooltipTrigger>
              <TooltipContent>{dateTime(k.createdAt, lang)}</TooltipContent>
            </Tooltip>
            {' · '}
            {k.lastUsedAt ? (
              <Tooltip>
                <TooltipTrigger className="cursor-default">{t('keys.usedAgo', { when: timeAgo(k.lastUsedAt, lang) })}</TooltipTrigger>
                <TooltipContent>{dateTime(k.lastUsedAt, lang)}</TooltipContent>
              </Tooltip>
            ) : (
              t('keys.neverUsed')
            )}
          </p>
          {!!k.dailyCreditLimit && (
            <p
              className={cn(
                'mt-0.5 text-xs tabular',
                (k.dailyCreditsUsed ?? 0) >= k.dailyCreditLimit ? 'text-destructive' : 'text-muted-foreground',
              )}
            >
              {t('cost.keyToday', { used: (k.dailyCreditsUsed ?? 0).toFixed(2), limit: k.dailyCreditLimit })}
            </p>
          )}
        </div>
      </div>
      <div className="grid grid-cols-[1fr_1fr_auto] items-end gap-4">
        <UsageMeter label={t('keys.tokens')} used={k.tokensUsed} limit={k.tokenLimit} format={compact} />
        <UsageMeter
          label={t('keys.credits')}
          used={k.creditsUsed}
          limit={k.creditLimit}
          format={(n) => (n >= 1000 ? compact(n) : n.toFixed(n < 10 ? 2 : 1))}
        />
        <div className="text-right">
          <p className="text-xs text-muted-foreground">{t('keys.requests')}</p>
          <p className="font-medium tabular">{compact(k.requestsCount)}</p>
        </div>
      </div>
      <div className="flex items-center justify-end gap-1">
        <Switch
          checked={k.enabled}
          aria-label={k.enabled ? t('common.disable') : t('common.enable')}
          onCheckedChange={(v) =>
            act(() => update.mutateAsync({ id: k.id, patch: { enabled: v } }), v ? t('keys.enabledToast') : t('keys.disabledToast'))
          }
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label={t('common.actions')}>
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil />
              {t('common.edit')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={async () => {
                if (await confirm({ title: t('keys.resetTitle', { name }), description: t('keys.resetBody'), confirmLabel: t('keys.resetUsage') }))
                  act(() => reset.mutateAsync(k.id), t('keys.usageReset'))
              }}
            >
              <RotateCcw />
              {t('keys.resetUsage')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={async () => {
                if (
                  await confirm({
                    title: t('keys.regenerateTitle', { name }),
                    description: t('keys.regenerateBody'),
                    confirmLabel: t('keys.regenerate'),
                    destructive: true,
                  })
                )
                  act(async () => {
                    const r = await update.mutateAsync({ id: k.id, patch: { regenerate: true } })
                    if (r.key) onRegenerated(r.key)
                  }, t('keys.regenerated'))
              }}
            >
              <RefreshCw />
              {t('keys.regenerate')}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onSelect={async () => {
                if (
                  await confirm({
                    title: t('keys.deleteTitle', { name }),
                    description: t('keys.deleteBody'),
                    confirmLabel: t('common.delete'),
                    destructive: true,
                  })
                )
                  act(() => remove.mutateAsync(k.id), t('keys.deleted'))
              }}
            >
              <Trash2 />
              {t('common.delete')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  )
}

export function ApiKeysCard({ onKeyCreated }: { onKeyCreated: (plaintext: string) => void }) {
  const { t } = useI18n()
  const keys = useApiKeys()
  const settings = useSettings()
  const [dialog, setDialog] = useState<{ open: boolean; editing: ApiKeyView | null }>({ open: false, editing: null })
  const [revealed, setRevealed] = useState<string | null>(null)
  const list = keys.data ?? []
  const pg = usePagination(list, { storageKey: 'keys' })
  const enabledCount = list.filter((k) => k.enabled).length
  const enforced = settings.data?.requireApiKey

  return (
    <Card className="gap-0 overflow-hidden py-0 shadow-xs">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-5 sm:px-6">
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground">
            <KeyRound className="size-[18px]" />
          </div>
          <div>
            <h3 className="flex items-center gap-2 font-semibold leading-tight">
              {t('keys.title')}
              {keys.data && (
                <Badge variant="secondary" className="tabular">
                  {enabledCount} / {list.length}
                </Badge>
              )}
            </h3>
            <p className="mt-1 text-sm text-muted-foreground">{t('keys.desc')}</p>
          </div>
        </div>
        <Button onClick={() => setDialog({ open: true, editing: null })}>
          <Plus />
          {t('keys.add')}
        </Button>
      </div>

      {settings.data && (
        <div
          className={cn(
            'flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-5 py-2.5 text-sm sm:px-6',
            enforced ? 'bg-success/[0.06]' : 'bg-warning/10',
          )}
        >
          {enforced ? <ShieldCheck className="size-4 text-success" /> : <ShieldAlert className="size-4 text-warning" />}
          <span className="font-medium">{enforced ? t('keys.enforced') : t('keys.notEnforced')}</span>
          <span className="text-muted-foreground">
            {enforced
              ? enabledCount === 0
                ? t('keys.enforcedNoKeys')
                : t('keys.enforcedHint')
              : t('keys.notEnforcedHint')}
          </span>
          <Link
            to="/settings"
            hash="sec-security"
            className="ml-auto text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            {t('keys.changeInSettings')}
          </Link>
        </div>
      )}

      {keys.isLoading ? (
        <div className="space-y-3 p-6">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : keys.isError ? (
        <div className="p-6 text-sm text-destructive">{(keys.error as Error).message}</div>
      ) : list.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t('keys.emptyTitle')}
          description={t('keys.emptyDesc')}
          action={
            <Button size="sm" onClick={() => setDialog({ open: true, editing: null })}>
              <Plus />
              {t('keys.add')}
            </Button>
          }
        />
      ) : (
        <>
          <ul className="stagger divide-y">
            {pg.pageItems.map((k) => (
              <KeyRow
                key={k.id}
                k={k}
                onEdit={() => setDialog({ open: true, editing: k })}
                onRegenerated={(plain) => {
                  setRevealed(plain)
                  onKeyCreated(plain)
                }}
              />
            ))}
          </ul>
          <TablePagination page={pg.page} pageCount={pg.pageCount} pageSize={pg.pageSize} total={pg.total} onPage={pg.setPage} onPageSize={pg.setPageSize} />
        </>
      )}

      <KeyDialog
        open={dialog.open}
        editing={dialog.editing}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        onCreated={(plain) => {
          setRevealed(plain)
          onKeyCreated(plain)
          toast.success(t('keys.created'))
        }}
      />
      <RevealDialog value={revealed} onClose={() => setRevealed(null)} />
    </Card>
  )
}
