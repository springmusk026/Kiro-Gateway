import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Card } from '@/components/ui/card'
import { useAnimatedNumber } from '@/hooks/use-animated-number'
import { cn } from '@/lib/utils'

export function StatCard({
  label,
  value,
  format = (n) => Math.round(n).toLocaleString(),
  icon: Icon,
  hint,
  tone = 'default',
  children,
}: {
  label: string
  value: number
  format?: (n: number) => string
  icon: LucideIcon
  hint?: ReactNode
  tone?: 'default' | 'success' | 'danger' | 'primary'
  children?: ReactNode
}) {
  const animated = useAnimatedNumber(value)
  return (
    <Card className="group relative gap-0 overflow-hidden p-5 transition-shadow hover:shadow-md">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[13px] font-medium text-muted-foreground">{label}</p>
          <p
            className={cn(
              'mt-1.5 text-3xl font-semibold tracking-tight tabular',
              tone === 'danger' && value > 0 && 'text-destructive',
              tone === 'success' && 'text-success',
            )}
          >
            {format(animated)}
          </p>
        </div>
        <div
          className={cn(
            'grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground transition-transform group-hover:scale-105',
            tone === 'danger' && 'bg-destructive/10 text-destructive',
            tone === 'success' && 'bg-success/12 text-success',
          )}
        >
          <Icon className="size-[18px]" />
        </div>
      </div>
      {hint && <div className="mt-2 text-xs text-muted-foreground">{hint}</div>}
      {children}
    </Card>
  )
}
