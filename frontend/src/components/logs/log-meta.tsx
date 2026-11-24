import { Ban, CircleAlert, CircleCheck, CircleHelp, CreditCard, Gauge, KeyRound, UserX, type LucideIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import type { RequestLog } from '@/lib/types'
import { cn } from '@/lib/utils'

export const ERROR_TYPES: { id: string; label: MessageKey; hint: MessageKey; icon: LucideIcon }[] = [
  { id: 'quota', label: 'logs.err.quota', hint: 'logs.err.quotaHint', icon: Gauge },
  { id: 'overage', label: 'logs.err.overage', hint: 'logs.err.overageHint', icon: CreditCard },
  { id: 'suspended', label: 'logs.err.suspended', hint: 'logs.err.suspendedHint', icon: Ban },
  { id: 'auth', label: 'logs.err.auth', hint: 'logs.err.authHint', icon: KeyRound },
  { id: 'profile', label: 'logs.err.profile', hint: 'logs.err.profileHint', icon: UserX },
  { id: 'unknown', label: 'logs.err.unknown', hint: 'logs.err.unknownHint', icon: CircleHelp },
]

export const ENDPOINT_LABEL: Record<string, MessageKey> = {
  claude: 'logs.ep.claude',
  openai: 'logs.ep.openai',
  responses: 'logs.ep.responses',
}

export function EndpointChip({ endpoint }: { endpoint: string }) {
  const { t } = useI18n()
  const key = ENDPOINT_LABEL[endpoint]
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border px-1.5 py-0.5 font-mono text-[11px]',
        endpoint === 'claude' && 'border-primary/30 bg-primary/[0.06] text-primary',
        endpoint === 'openai' && 'border-success/30 bg-success/[0.07] text-success',
        endpoint === 'responses' && 'border-info/30 bg-info/[0.07] text-info',
      )}
    >
      {key ? t(key) : endpoint || '–'}
    </span>
  )
}

export function StatusBadge({ log }: { log: RequestLog }) {
  const { t } = useI18n()
  if (log.status === 'success') {
    return (
      <Badge variant="outline" className="border-success/30 bg-success/[0.08] text-success">
        <CircleCheck />
        {t('logs.success')}
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="border-destructive/30 bg-destructive/[0.08] text-destructive">
      <CircleAlert />
      {t('logs.error')}
    </Badge>
  )
}

export function ErrorTypeBadge({ type }: { type: string }) {
  const { t } = useI18n()
  if (!type) return null
  const meta = ERROR_TYPES.find((e) => e.id === type) ?? ERROR_TYPES[ERROR_TYPES.length - 1]
  return (
    <Badge
      variant="outline"
      className={cn(
        'font-normal',
        (type === 'quota' || type === 'overage') && 'border-warning/40 text-warning',
        (type === 'suspended' || type === 'auth') && 'border-destructive/30 text-destructive',
      )}
    >
      <meta.icon />
      {t(meta.label)}
    </Badge>
  )
}
