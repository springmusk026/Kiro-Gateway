import { Boxes } from 'lucide-react'
import { useMemo, useState } from 'react'
import { EmptyState } from '@/components/app/empty-state'
import { usePrivacyMode } from '@/components/accounts/lib'
import { shortUA } from '@/components/logs/history-bits'
import { ENDPOINT_LABEL } from '@/components/logs/log-meta'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { compact, maskEmail } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useUsageBy } from '@/lib/queries.history'
import { useApiKeys } from '@/lib/queries.settings'
import type { UsageDimension } from '@/lib/types'
import type { MessageKey } from '@/locales/en'
import { cn } from '@/lib/utils'

const DIMS: { id: UsageDimension; label: MessageKey }[] = [
  { id: 'model', label: 'overview.u.dimModels' },
  { id: 'account', label: 'overview.u.dimAccounts' },
  { id: 'apiKey', label: 'overview.u.dimKeys' },
  { id: 'client', label: 'overview.u.dimClients' },
]

/** Top groups by requests from GET /usage/by, with error rate and tokens. */
export function UsageBreakdown({ hours }: { hours: number }) {
  const { t } = useI18n()
  const [dim, setDim] = useState<UsageDimension>('model')
  const [privacy] = usePrivacyMode()
  const groups = useUsageBy(dim, hours, 6)
  const keys = useApiKeys()
  const keyNames = useMemo(() => {
    const m: Record<string, string> = {}
    for (const k of keys.data ?? []) m[k.id] = k.name || k.keyHint
    return m
  }, [keys.data])

  const label = (key: string): { text: string; title?: string; muted?: boolean } => {
    if (!key) return { text: dim === 'apiKey' ? t('overview.u.noKey') : t('overview.u.unknown'), muted: true }
    if (dim === 'apiKey') return { text: keyNames[key] || key.slice(0, 8), title: key }
    if (dim === 'client') return { text: shortUA(key), title: key }
    if (dim === 'account') return { text: privacy && key.includes('@') ? maskEmail(key) : key }
    if (dim === 'endpoint') return { text: ENDPOINT_LABEL[key] ? t(ENDPOINT_LABEL[key]) : key }
    return { text: key }
  }

  const rows = groups.data ?? []
  const max = rows.reduce((m, g) => Math.max(m, g.requests), 0)

  return (
    <div className="space-y-4">
      <Tabs value={dim} onValueChange={(v) => setDim(v as UsageDimension)}>
        <TabsList className="w-full">
          {DIMS.map((d) => (
            <TabsTrigger key={d.id} value={d.id} className="flex-1 text-xs">
              {t(d.label)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      {groups.isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : !rows.length ? (
        <EmptyState icon={Boxes} title={t('overview.u.noGroups')} />
      ) : (
        <ul key={dim} className={cn('space-y-3 stagger', groups.isPlaceholderData && 'opacity-60')}>
          {rows.map((g) => {
            const l = label(g.key)
            const errRate = g.requests ? (100 * g.errors) / g.requests : 0
            return (
              <li key={g.key || '∅'}>
                <div className="mb-1 flex items-center justify-between gap-3 text-sm">
                  <span
                    className={cn('min-w-0 truncate', dim === 'model' || dim === 'apiKey' ? 'font-mono text-[13px]' : 'text-[13px]', l.muted && 'italic text-muted-foreground')}
                    title={l.title}
                  >
                    {l.text}
                  </span>
                  <span className="shrink-0 tabular text-muted-foreground">{g.requests.toLocaleString()}</span>
                </div>
                <div className="flex h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full bg-gradient-to-r from-chart-1 to-chart-2 transition-[width] duration-700"
                    style={{ width: `${(100 * (g.requests - g.errors)) / (max || 1)}%` }}
                  />
                  <div className="h-full bg-destructive/80 transition-[width] duration-700" style={{ width: `${(100 * g.errors) / (max || 1)}%` }} />
                </div>
                <div className="mt-1 flex items-center justify-between gap-3 text-[11px] text-muted-foreground tabular">
                  <span className={cn(errRate >= 20 ? 'text-destructive' : errRate >= 5 && 'text-warning')}>
                    {t('overview.u.errRate', { n: errRate.toFixed(errRate && errRate < 10 ? 1 : 0) })}
                  </span>
                  <span>{t('overview.u.tokensShort', { in: compact(g.inputTokens), out: compact(g.outputTokens) })}</span>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
