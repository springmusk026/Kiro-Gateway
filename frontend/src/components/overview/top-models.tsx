import { Boxes } from 'lucide-react'
import { useMemo } from 'react'
import { EmptyState } from '@/components/app/empty-state'
import { useI18n } from '@/lib/i18n'
import type { RequestLog } from '@/lib/types'

export function TopModels({ logs }: { logs: RequestLog[] }) {
  const { t } = useI18n()
  const rows = useMemo(() => {
    const m = new Map<string, number>()
    for (const l of logs) if (l.model) m.set(l.model, (m.get(l.model) ?? 0) + 1)
    return [...m.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  }, [logs])
  if (!rows.length) return <EmptyState icon={Boxes} title={t('overview.noModels')} />
  const max = rows[0][1]
  return (
    <ul className="space-y-3 stagger">
      {rows.map(([model, n]) => (
        <li key={model}>
          <div className="mb-1 flex items-center justify-between gap-3 text-sm">
            <span className="truncate font-mono text-[13px]">{model}</span>
            <span className="tabular text-muted-foreground">{n}</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-gradient-to-r from-chart-1 to-chart-2 transition-[width] duration-700" style={{ width: `${(100 * n) / max}%` }} />
          </div>
        </li>
      ))}
    </ul>
  )
}
