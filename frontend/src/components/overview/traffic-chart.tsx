import { Activity } from 'lucide-react'
import { useMemo } from 'react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { EmptyState } from '@/components/app/empty-state'
import { useI18n } from '@/lib/i18n'
import type { RequestLog } from '@/lib/types'

const MINUTES = 60

export function TrafficChart({ logs }: { logs: RequestLog[] }) {
  const { t, lang } = useI18n()
  const data = useMemo(() => {
    const now = Math.floor(Date.now() / 60000)
    const rows = Array.from({ length: MINUTES }, (_, i) => {
      const minute = now - (MINUTES - 1 - i)
      return { minute, label: new Date(minute * 60000).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' }), ok: 0, err: 0 }
    })
    for (const l of logs) {
      const idx = MINUTES - 1 - (now - Math.floor(l.time / 60))
      if (idx < 0 || idx >= MINUTES) continue
      if (l.status === 'success') rows[idx].ok++
      else rows[idx].err++
    }
    return rows
  }, [logs, lang])
  const total = data.reduce((s, r) => s + r.ok + r.err, 0)

  if (!total) {
    return <EmptyState icon={Activity} title={t('overview.noTraffic')} description={t('overview.noTrafficHint')} />
  }
  return (
    <div className="h-64 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
          <defs>
            <linearGradient id="okFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--chart-1)" stopOpacity={0.35} />
              <stop offset="100%" stopColor="var(--chart-1)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="errFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--destructive)" stopOpacity={0.35} />
              <stop offset="100%" stopColor="var(--destructive)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 4" />
          <XAxis dataKey="label" tickLine={false} axisLine={false} interval={14} tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }} />
          <YAxis allowDecimals={false} tickLine={false} axisLine={false} tick={{ fill: 'var(--muted-foreground)', fontSize: 11 }} />
          <Tooltip
            cursor={{ stroke: 'var(--border)' }}
            contentStyle={{ background: 'var(--popover)', border: '1px solid var(--border)', borderRadius: 10, fontSize: 12 }}
            labelStyle={{ color: 'var(--muted-foreground)' }}
          />
          <Area type="monotone" dataKey="ok" name={t('overview.success')} stackId="1" stroke="var(--chart-1)" strokeWidth={2} fill="url(#okFill)" animationDuration={700} />
          <Area type="monotone" dataKey="err" name={t('overview.errors')} stackId="1" stroke="var(--destructive)" strokeWidth={2} fill="url(#errFill)" animationDuration={700} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
