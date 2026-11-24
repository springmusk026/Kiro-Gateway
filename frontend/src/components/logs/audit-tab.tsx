import {
  Activity,
  Filter,
  KeyRound,
  ScrollText,
  Search,
  Settings2,
  ShieldAlert,
  Trash2,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { TablePagination } from '@/components/app/table-pagination'
import { usePagination } from '@/hooks/use-pagination'
import { EmptyState } from '@/components/app/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useAudit } from '@/lib/queries.history'
import type { AuditEvent } from '@/lib/types'
import type { MessageKey } from '@/locales/en'
import { cn } from '@/lib/utils'
import { exactTime, unix } from './history-bits'

type Family = { id: string; icon: LucideIcon; label: MessageKey; tone: string }

const FAMILIES: Record<string, Family> = {
  security: { id: 'security', icon: ShieldAlert, label: 'logs.audit.famSecurity', tone: 'bg-destructive/10 text-destructive ring-destructive/25' },
  account: { id: 'account', icon: Users, label: 'logs.audit.famAccount', tone: 'bg-primary/10 text-primary ring-primary/20' },
  key: { id: 'key', icon: KeyRound, label: 'logs.audit.famKey', tone: 'bg-info/10 text-info ring-info/25' },
  settings: { id: 'settings', icon: Settings2, label: 'logs.audit.famSettings', tone: 'bg-accent text-accent-foreground ring-border' },
  maintenance: { id: 'maintenance', icon: Trash2, label: 'logs.audit.famMaintenance', tone: 'bg-warning/12 text-warning ring-warning/30' },
  other: { id: 'other', icon: Activity, label: 'logs.audit.famOther', tone: 'bg-muted text-muted-foreground ring-border' },
}

const SETTINGS_PATHS = ['/settings', '/thinking', '/endpoint', '/proxy', '/model-mappings', '/prompt-filter', '/logging', '/models']

function splitAction(action: string): { method: string; path: string } {
  const m = action.match(/^(GET|POST|PUT|PATCH|DELETE)\s+(.*)$/)
  return m ? { method: m[1], path: m[2] } : { method: '', path: action }
}

function familyOf(e: AuditEvent): Family {
  if (e.action.startsWith('login.')) return FAMILIES.security
  const { path } = splitAction(e.action)
  if (path.startsWith('/accounts')) return FAMILIES.account
  if (path.startsWith('/api-keys')) return FAMILIES.key
  if (path.startsWith('/logs') || path.startsWith('/stats')) return FAMILIES.maintenance
  if (SETTINGS_PATHS.some((p) => path.startsWith(p))) return FAMILIES.settings
  return FAMILIES.other
}

const AUDIT_RANGES = [
  { id: '24', hours: 24, label: 'logs.range.24h' },
  { id: '168', hours: 168, label: 'logs.range.7d' },
  { id: '720', hours: 720, label: 'logs.range.30d' },
] as const satisfies readonly { id: string; hours: number; label: MessageKey }[]

function MethodChip({ method }: { method: string }) {
  if (!method) return null
  return (
    <span
      className={cn(
        'inline-flex h-5 items-center rounded px-1.5 font-mono text-[10.5px] font-semibold',
        method === 'POST' && 'bg-success/12 text-success',
        method === 'PUT' && 'bg-info/12 text-info',
        method === 'PATCH' && 'bg-info/12 text-info',
        method === 'DELETE' && 'bg-destructive/10 text-destructive',
        method === 'GET' && 'bg-muted text-muted-foreground',
      )}
    >
      {method}
    </span>
  )
}

export function AuditTab() {
  const { t, lang } = useI18n()
  const [hours, setHours] = useState(168)
  const [search, setSearch] = useState('')
  const audit = useAudit(hours, 500)

  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase()
    const list = audit.data ?? []
    if (!s) return list
    return list.filter((e) =>
      `${e.action} ${e.actor} ${e.ip ?? ''} ${e.target ?? ''} ${Object.entries(e.detail ?? {}).flat().join(' ')} ${t(familyOf(e).label)}`
        .toLowerCase()
        .includes(s),
    )
  }, [audit.data, search, t])

  const pg = usePagination(filtered, { storageKey: 'audit', pageSize: 50, resetKey: search })
  const days = useMemo(() => {
    const out: { day: string; events: AuditEvent[] }[] = []
    for (const e of pg.pageItems) {
      const day = new Date(e.time).toLocaleDateString(lang, { weekday: 'short', year: 'numeric', month: 'short', day: 'numeric' })
      const last = out[out.length - 1]
      if (last && last.day === day) last.events.push(e)
      else out.push({ day, events: [e] })
    }
    return out
  }, [pg.pageItems, lang])

  const failedLogins = (audit.data ?? []).filter((e) => e.action === 'login.failed').length

  return (
    <Card className="gap-0 overflow-hidden py-0 shadow-xs">
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <div className="relative min-w-[220px] flex-1">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9"
            placeholder={t('logs.audit.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label={t('logs.audit.searchPlaceholder')}
          />
        </div>
        {failedLogins > 0 && (
          <Badge variant="outline" className="h-8 border-destructive/30 bg-destructive/[0.06] px-2.5 text-destructive">
            <ShieldAlert />
            {t('logs.audit.failedLogins', { n: failedLogins })}
          </Badge>
        )}
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={String(hours)}
          onValueChange={(v) => v && setHours(Number(v))}
          aria-label={t('logs.h.range')}
        >
          {AUDIT_RANGES.map((r) => (
            <ToggleGroupItem key={r.id} value={String(r.hours)} className="px-3 text-xs">
              {t(r.label)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      {audit.isLoading ? (
        <div className="space-y-3 p-5">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex items-center gap-3">
              <Skeleton className="size-8 rounded-full" />
              <Skeleton className="h-10 flex-1" />
            </div>
          ))}
        </div>
      ) : audit.isError ? (
        <EmptyState
          icon={ScrollText}
          title={t('common.error')}
          description={(audit.error as Error).message}
          action={
            <Button size="sm" variant="outline" onClick={() => audit.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      ) : !audit.data?.length ? (
        <EmptyState icon={ScrollText} title={t('logs.audit.emptyTitle')} description={t('logs.audit.emptyDesc')} />
      ) : !filtered.length ? (
        <EmptyState
          icon={Filter}
          title={t('logs.noMatchTitle')}
          description={t('logs.audit.noMatchDesc')}
          action={
            <Button size="sm" variant="outline" onClick={() => setSearch('')}>
              {t('logs.resetFilters')}
            </Button>
          }
        />
      ) : (
        <div className={cn('px-4 pb-4 sm:px-5', audit.isPlaceholderData && 'opacity-60')}>
          {days.map((d) => (
            <section key={d.day}>
              <h3 className="pb-2 pt-4 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {d.day}
              </h3>
              <ol className="relative ml-4 border-l stagger">
                {d.events.map((e, i) => {
                  const fam = familyOf(e)
                  const { method, path } = splitAction(e.action)
                  const status = e.detail?.status
                  const detail = Object.entries(e.detail ?? {}).filter(([k]) => k !== 'status')
                  return (
                    <li key={`${e.time}-${i}`} className="relative pb-3 pl-7 last:pb-1">
                      <span
                        className={cn('absolute -left-4 top-1 grid size-8 place-items-center rounded-full ring-1', fam.tone)}
                        aria-hidden
                      >
                        <fam.icon className="size-4" />
                      </span>
                      <div
                        className={cn(
                          'flex flex-col gap-1 rounded-lg px-3 py-2 transition-colors hover:bg-muted/40 sm:flex-row sm:items-center sm:gap-4',
                          fam.id === 'security' && 'bg-destructive/[0.04]',
                        )}
                      >
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <MethodChip method={method} />
                            <span className={cn('truncate font-mono text-[13px]', fam.id === 'security' && 'font-medium text-destructive')}>
                              {fam.id === 'security' ? t('logs.audit.loginFailed') : path}
                            </span>
                            {status && (
                              <Badge
                                variant="outline"
                                className={cn('h-5 font-mono text-[10.5px]', Number(status) >= 400 && 'border-destructive/30 text-destructive')}
                              >
                                {status}
                              </Badge>
                            )}
                            {e.target && <span className="truncate font-mono text-xs text-muted-foreground">{e.target}</span>}
                          </div>
                          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                            <span>{t(fam.label)}</span>
                            <span aria-hidden>·</span>
                            <span>{e.actor || '–'}</span>
                            {e.ip && (
                              <>
                                <span aria-hidden>·</span>
                                <span className="font-mono">{e.ip}</span>
                              </>
                            )}
                            {detail.map(([k, v]) => (
                              <span key={k} className="rounded bg-muted px-1.5 font-mono text-[11px]">
                                {k}={v}
                              </span>
                            ))}
                          </p>
                        </div>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="shrink-0 text-xs text-muted-foreground tabular">
                              {new Date(e.time).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                              <span className="ml-2 hidden sm:inline">({timeAgo(unix(e.time), lang)})</span>
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>{exactTime(e.time, lang)}</TooltipContent>
                        </Tooltip>
                      </div>
                    </li>
                  )
                })}
              </ol>
            </section>
          ))}
          <p className="pt-3 text-center text-xs text-muted-foreground tabular">
            {t('logs.audit.count', { n: filtered.length })}
          </p>
          <TablePagination className="-mx-4 mt-2 sm:-mx-5" page={pg.page} pageCount={pg.pageCount} pageSize={pg.pageSize} total={pg.total} onPage={pg.setPage} onPageSize={pg.setPageSize} />
        </div>
      )}
    </Card>
  )
}
