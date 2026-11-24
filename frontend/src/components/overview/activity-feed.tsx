import { Link } from '@tanstack/react-router'
import { CheckCircle2, Radio, XCircle } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { EmptyState } from '@/components/app/empty-state'
import { Badge } from '@/components/ui/badge'
import { compact, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { RequestLog } from '@/lib/types'
import { cn } from '@/lib/utils'

const key = (l: RequestLog) => `${l.time}-${l.model}-${l.accountId}-${l.duration}-${l.tokens}`

export function ActivityFeed({ logs }: { logs: RequestLog[] }) {
  const { t, lang } = useI18n()
  const rows = [...logs].sort((a, b) => b.time - a.time).slice(0, 8)
  // Highlight rows that appeared since the previous poll.
  const seen = useRef<Set<string> | null>(null)
  const [fresh, setFresh] = useState<Set<string>>(new Set())
  useEffect(() => {
    const keys = rows.map(key)
    const prev = seen.current
    seen.current = new Set(keys)
    if (!prev) return
    const added = keys.filter((k) => !prev.has(k))
    if (!added.length) return
    setFresh(new Set(added))
    const id = setTimeout(() => setFresh(new Set()), 2200)
    return () => clearTimeout(id)
  }, [logs])

  if (!rows.length) return <EmptyState icon={Radio} title={t('overview.noActivity')} />
  return (
    <ul className="divide-y">
      {rows.map((l) => {
        const k = key(l)
        const ok = l.status === 'success'
        return (
          <li
            key={k}
            className={cn('flex items-center gap-3 py-2.5 transition-colors duration-1000', fresh.has(k) && 'animate-rise bg-accent/60')}
          >
            {ok ? <CheckCircle2 className="size-4 shrink-0 text-success" /> : <XCircle className="size-4 shrink-0 text-destructive" />}
            <div className="min-w-0 flex-1">
              <p className="truncate font-mono text-[13px]">{l.model || '—'}</p>
              <p className="truncate text-xs text-muted-foreground">
                {ok ? `${compact(l.tokens)} tokens · ${(l.duration / 1000).toFixed(1)}s` : l.error || l.errorType}
              </p>
            </div>
            <Badge variant="secondary" className="hidden font-normal sm:inline-flex">
              {l.endpoint}
            </Badge>
            <span className="w-16 shrink-0 text-right text-xs text-muted-foreground">{timeAgo(l.time, lang)}</span>
          </li>
        )
      })}
      <li className="pt-3">
        <Link to="/logs" className="text-sm font-medium text-primary hover:underline">
          {t('overview.viewAll')} →
        </Link>
      </li>
    </ul>
  )
}
