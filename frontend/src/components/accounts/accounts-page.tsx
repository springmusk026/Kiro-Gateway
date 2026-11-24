import { useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowDownUp,
  Ban,
  CheckCircle2,
  Download,
  Eye,
  EyeOff,
  Loader2,
  MoreHorizontal,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  SearchX,
  Users,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { EmptyState } from '@/components/app/empty-state'
import { PageHeader } from '@/components/app/page-header'
import { useConfirm } from '@/components/app/confirm'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { accountApi, invalidateAccounts, useAccounts, useRefreshAllModels } from '@/lib/queries.accounts'
import type { NewAccountRef } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AccountDetailSheet } from './account-detail'
import { AccountsCards, AccountsTable } from './accounts-list'
import { AccountActionsProvider, type DetailTab } from './actions'
import { AddAccountDialog } from './add/add-account-dialog'
import { BatchBar } from './batch-bar'
import { ExportDialog } from './export-dialog'
import { TestDialog } from './test-dialog'
import {
  errMsg,
  isAvailable,
  isBanned,
  matchesSearch,
  matchesStatus,
  needsAttention,
  sortAccounts,
  usePrivacyMode,
  type SortKey,
  type StatusFilter,
} from './lib'

const SORT_KEY = 'kiro_accounts_sort'
const readSort = (): SortKey => {
  try {
    return (localStorage.getItem(SORT_KEY) as SortKey) || 'default'
  } catch {
    return 'default'
  }
}

function SummaryChip({
  icon: Icon,
  label,
  value,
  active,
  tone,
  onClick,
}: {
  icon: LucideIcon
  label: string
  value: number
  active: boolean
  tone?: 'success' | 'danger' | 'warning'
  onClick: () => void
}) {
  const n = useAnimatedNumber(value)
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'group flex items-center gap-2.5 rounded-xl border bg-card px-3 py-2 text-left transition-all hover:border-primary/40 hover:shadow-xs',
        active && 'border-primary/60 bg-primary/5 ring-1 ring-primary/30',
      )}
    >
      <span
        className={cn(
          'grid size-7 place-items-center rounded-lg bg-accent text-accent-foreground',
          tone === 'success' && 'bg-success/12 text-success',
          tone === 'danger' && value > 0 && 'bg-destructive/10 text-destructive',
          tone === 'warning' && value > 0 && 'bg-warning/15 text-warning',
        )}
      >
        <Icon className="size-3.5" />
      </span>
      <span>
        <span className="block text-lg font-semibold leading-none tabular">{Math.round(n)}</span>
        <span className="mt-0.5 block text-[11px] text-muted-foreground">{label}</span>
      </span>
    </button>
  )
}

function ListSkeleton() {
  return (
    <Card className="gap-0 overflow-hidden p-0">
      <div className="border-b bg-muted/40 px-4 py-3">
        <Skeleton className="h-4 w-40" />
      </div>
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex items-center gap-4 border-b px-4 py-3.5 last:border-0">
          <Skeleton className="size-4 rounded" />
          <div className="flex flex-1 items-center gap-2.5">
            <Skeleton className="size-2 rounded-full" />
            <div className="space-y-1.5">
              <Skeleton className="h-4 w-44" />
              <Skeleton className="h-3 w-24" />
            </div>
          </div>
          <Skeleton className="hidden h-5 w-16 rounded-md sm:block" />
          <Skeleton className="hidden h-5 w-20 rounded-md md:block" />
          <div className="hidden w-40 space-y-1.5 lg:block">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-1.5 w-full" />
          </div>
          <Skeleton className="h-5 w-8 rounded-full" />
        </div>
      ))}
    </Card>
  )
}

export function AccountsPage() {
  const { t, lang } = useI18n()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const q = useAccounts()
  const refreshAll = useRefreshAllModels()
  const [privacy, setPrivacy] = usePrivacyMode()

  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [sort, setSortState] = useState<SortKey>(readSort)
  const setSort = (s: SortKey) => {
    setSortState(s)
    try {
      localStorage.setItem(SORT_KEY, s)
    } catch {
      /* storage unavailable */
    }
  }
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [highlight, setHighlight] = useState<Set<string>>(new Set())
  const [detail, setDetail] = useState<{ id: string; tab: DetailTab } | null>(null)
  const [testId, setTestId] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [exportState, setExportState] = useState<{ open: boolean; ids?: string[] }>({ open: false })

  const accounts = useMemo(() => q.data ?? [], [q.data])
  const byId = useMemo(() => new Map(accounts.map((a) => [a.id, a])), [accounts])
  const visible = useMemo(
    () => sortAccounts(accounts.filter((a) => matchesStatus(a, status) && matchesSearch(a, search.trim())), sort),
    [accounts, status, search, sort],
  )
  const counts = useMemo(
    () => ({
      total: accounts.length,
      enabled: accounts.filter((a) => a.enabled).length,
      available: accounts.filter(isAvailable).length,
      banned: accounts.filter(isBanned).length,
      attention: accounts.filter(needsAttention).length,
    }),
    [accounts],
  )

  // Drop selections for accounts that no longer exist.
  useEffect(() => {
    setSelected((s) => {
      const n = new Set([...s].filter((id) => byId.has(id)))
      return n.size === s.size ? s : n
    })
  }, [byId])

  useEffect(() => {
    if (!highlight.size) return
    const tm = setTimeout(() => setHighlight(new Set()), 6000)
    return () => clearTimeout(tm)
  }, [highlight])

  const openDetail = useCallback((id: string, tab: DetailTab = 'overview') => setDetail({ id, tab }), [])
  const openTest = useCallback((id: string) => setTestId(id), [])

  const onAdded = useCallback(
    (refs: NewAccountRef[]) => {
      if (!refs.length) return
      setStatus('all')
      setSearch('')
      setHighlight(new Set(refs.map((r) => r.id)))
      invalidateAccounts(qc)
      // Populate usage / subscription for the new accounts in the background.
      ;(async () => {
        for (const r of refs) {
          try {
            await accountApi.refresh(r.id)
          } catch {
            /* ignored: the row shows its state */
          }
          invalidateAccounts(qc)
        }
      })()
    },
    [qc],
  )

  const doRefreshAll = async () => {
    const ok = await confirm({
      title: t('accounts.refreshAll.title'),
      description: t('accounts.refreshAll.text'),
      confirmLabel: t('accounts.refreshAll.confirm'),
    })
    if (!ok) return
    try {
      const r = await refreshAll.mutateAsync()
      toast.success(t('accounts.refreshAll.done', { n: r.refreshed }))
    } catch (e) {
      toast.error(errMsg(e))
    }
  }

  const visibleIds = visible.map((a) => a.id)
  const selectAll = (on: boolean) =>
    setSelected((s) => {
      if (!on) return new Set()
      return new Set([...s, ...visibleIds])
    })
  const selectOne = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s)
      if (on) n.add(id)
      else n.delete(id)
      return n
    })

  const filtersActive = status !== 'all' || !!search.trim()
  const chip = (f: StatusFilter) => () => setStatus((s) => (s === f ? 'all' : f))

  return (
    <AccountActionsProvider openDetail={openDetail} openTest={openTest}>
      <div className={cn('space-y-6', selected.size > 0 && 'pb-20')}>
        <PageHeader
          title={t('nav.accounts')}
          description={t('page.accounts')}
          actions={
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => setPrivacy(!privacy)}
                    aria-pressed={privacy}
                    aria-label={privacy ? t('accounts.privacy.off') : t('accounts.privacy.on')}
                  >
                    {privacy ? <EyeOff /> : <Eye />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{privacy ? t('accounts.privacy.off') : t('accounts.privacy.on')}</TooltipContent>
              </Tooltip>
              <Button variant="outline" onClick={() => setExportState({ open: true })} disabled={!accounts.length} className="hidden sm:inline-flex">
                <Download /> {t('accounts.export.button')}
              </Button>
              <Button variant="outline" onClick={doRefreshAll} disabled={refreshAll.isPending || !accounts.length} className="hidden sm:inline-flex">
                {refreshAll.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />} {t('accounts.refreshAll.button')}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="icon" className="sm:hidden" aria-label={t('common.actions')}>
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => setExportState({ open: true })} disabled={!accounts.length}>
                    <Download /> {t('accounts.export.button')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={doRefreshAll} disabled={refreshAll.isPending || !accounts.length}>
                    <RotateCcw /> {t('accounts.refreshAll.button')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button onClick={() => setAddOpen(true)}>
                <Plus /> {t('accounts.add.button')}
              </Button>
            </>
          }
        />

        {q.isLoading ? (
          <>
            <div className="flex flex-wrap gap-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-[50px] w-32 rounded-xl" />
              ))}
            </div>
            <ListSkeleton />
          </>
        ) : q.isError && !q.data ? (
          <Card className="p-6">
            <Alert variant="destructive" className="border-0 p-0">
              <AlertTriangle />
              <AlertTitle>{t('accounts.error.title')}</AlertTitle>
              <AlertDescription>
                <p>{errMsg(q.error)}</p>
                <Button variant="outline" size="sm" className="mt-3" onClick={() => q.refetch()} disabled={q.isFetching}>
                  {q.isFetching ? <Loader2 className="animate-spin" /> : <RefreshCw />} {t('common.retry')}
                </Button>
              </AlertDescription>
            </Alert>
          </Card>
        ) : !accounts.length ? (
          <Card className="surface-glow">
            <EmptyState
              icon={Users}
              title={t('accounts.empty.title')}
              description={t('accounts.empty.text')}
              action={
                <Button onClick={() => setAddOpen(true)}>
                  <Plus /> {t('accounts.add.button')}
                </Button>
              }
            />
          </Card>
        ) : (
          <>
            <div className="flex flex-wrap gap-2 stagger">
              <SummaryChip icon={Users} label={t('accounts.summary.total')} value={counts.total} active={status === 'all'} onClick={() => setStatus('all')} />
              <SummaryChip icon={CheckCircle2} label={t('accounts.summary.enabled')} value={counts.enabled} active={status === 'enabled'} onClick={chip('enabled')} />
              <SummaryChip icon={Zap} tone="success" label={t('accounts.summary.available')} value={counts.available} active={status === 'available'} onClick={chip('available')} />
              <SummaryChip icon={Ban} tone="danger" label={t('accounts.summary.banned')} value={counts.banned} active={status === 'banned'} onClick={chip('banned')} />
              <SummaryChip icon={AlertTriangle} tone="warning" label={t('accounts.summary.attention')} value={counts.attention} active={status === 'attention'} onClick={chip('attention')} />
            </div>

            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <div className="relative flex-1 sm:max-w-sm">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={t('accounts.searchPlaceholder')}
                  className="pl-9"
                  aria-label={t('accounts.searchPlaceholder')}
                />
              </div>
              <div className="flex gap-2">
                <Select value={status} onValueChange={(v) => setStatus(v as StatusFilter)}>
                  <SelectTrigger className="w-full sm:w-40" aria-label={t('accounts.filter.label')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(['all', 'enabled', 'disabled', 'banned', 'available', 'attention'] as const).map((f) => (
                      <SelectItem key={f} value={f}>
                        {t(`accounts.filter.${f}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select value={sort} onValueChange={(v) => setSort(v as SortKey)}>
                  <SelectTrigger className="w-full sm:w-44" aria-label={t('accounts.sort.label')}>
                    <ArrowDownUp className="size-3.5 text-muted-foreground" />
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(['default', 'health', 'name', 'usage', 'requests', 'lastUsed', 'expiry', 'weight'] as const).map((s) => (
                      <SelectItem key={s} value={s}>
                        {t(`accounts.sort.${s}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground sm:ml-auto">
                <span className={cn('size-1.5 rounded-full', q.isError ? 'bg-destructive' : 'bg-success text-success animate-pulse-ring')} />
                <span>
                  {q.isError
                    ? t('accounts.live.error')
                    : t('accounts.live.updated', { when: timeAgo(Math.floor(q.dataUpdatedAt / 1000), lang) })}
                </span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button variant="ghost" size="icon" className="size-7" onClick={() => q.refetch()} aria-label={t('common.refresh')}>
                      <RefreshCw className={cn('size-3.5', q.isFetching && 'animate-spin')} />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t('common.refresh')}</TooltipContent>
                </Tooltip>
              </div>
            </div>

            {visible.length === 0 ? (
              <Card>
                <EmptyState
                  icon={SearchX}
                  title={t('accounts.empty.noMatch')}
                  description={t('accounts.empty.noMatchText')}
                  action={
                    filtersActive && (
                      <Button
                        variant="outline"
                        onClick={() => {
                          setSearch('')
                          setStatus('all')
                        }}
                      >
                        {t('accounts.empty.clearFilters')}
                      </Button>
                    )
                  }
                />
              </Card>
            ) : (
              <>
                <AccountsTable accounts={visible} selected={selected} onSelect={selectOne} onSelectAll={selectAll} highlight={highlight} />
                <AccountsCards accounts={visible} selected={selected} onSelect={selectOne} onSelectAll={selectAll} highlight={highlight} />
                <p className="text-center text-xs text-muted-foreground">
                  {t('accounts.showing', { n: visible.length, total: accounts.length })}
                  {visible.length > 0 && (
                    <>
                      {' · '}
                      <button type="button" className="underline-offset-4 hover:text-foreground hover:underline" onClick={() => selectAll(true)}>
                        {t('accounts.selectAllShown')}
                      </button>
                    </>
                  )}
                </p>
              </>
            )}
          </>
        )}
      </div>

      <BatchBar
        ids={[...selected]}
        onClear={() => setSelected(new Set())}
        onExport={() => setExportState({ open: true, ids: [...selected] })}
      />

      <AccountDetailSheet
        account={detail ? byId.get(detail.id) : undefined}
        tab={detail?.tab ?? 'overview'}
        onTabChange={(tab) => setDetail((d) => (d ? { ...d, tab } : d))}
        onClose={() => setDetail(null)}
      />
      <TestDialog account={testId ? byId.get(testId) : undefined} onClose={() => setTestId(null)} />
      <AddAccountDialog open={addOpen} onOpenChange={setAddOpen} onAdded={onAdded} />
      <ExportDialog
        open={exportState.open}
        onOpenChange={(o) => setExportState((s) => ({ ...s, open: o }))}
        accounts={accounts}
        filteredIds={visibleIds}
        initialIds={exportState.ids}
      />
    </AccountActionsProvider>
  )
}
