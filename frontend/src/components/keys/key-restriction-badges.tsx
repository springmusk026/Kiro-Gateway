import { CalendarClock, Gauge, ListFilter, TimerOff } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { dateTime } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import type { ApiKeyView } from '@/lib/types'
import { cn } from '@/lib/utils'

const SOON = 7 * 86400

/** Badges for a key's access restrictions in the keys list. */
export function RestrictionBadges({ k }: { k: ApiKeyView }) {
  const { t, lang } = useI18n()
  const now = Date.now() / 1000
  const expired = k.expired || (!!k.expiresAt && k.expiresAt <= now)
  const date = k.expiresAt ? new Date(k.expiresAt * 1000).toLocaleDateString(lang) : ''
  const models = k.allowedModels ?? []
  return (
    <>
      {k.expiresAt ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant="outline"
              className={cn(
                'gap-1',
                expired
                  ? 'border-destructive/40 text-destructive'
                  : k.expiresAt - now < SOON
                    ? 'border-warning/50 text-warning'
                    : 'text-muted-foreground',
              )}
            >
              {expired ? <TimerOff className="size-3" /> : <CalendarClock className="size-3" />}
              {expired ? t('keysec.badgeExpired') : t('keysec.badgeExpires', { date })}
            </Badge>
          </TooltipTrigger>
          <TooltipContent>{expired ? t('keysec.expiredHint', { date: dateTime(k.expiresAt, lang) }) : dateTime(k.expiresAt, lang)}</TooltipContent>
        </Tooltip>
      ) : null}
      {k.rpmLimit ? (
        <Badge variant="outline" className="gap-1 tabular text-muted-foreground">
          <Gauge className="size-3" />
          {t('keysec.badgeRpm', { n: k.rpmLimit })}
        </Badge>
      ) : null}
      {models.length > 0 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="gap-1 text-muted-foreground">
              <ListFilter className="size-3" />
              {models.length === 1 ? <span className="font-mono">{models[0]}</span> : t('keysec.badgeModels', { n: models.length })}
            </Badge>
          </TooltipTrigger>
          <TooltipContent className="max-w-xs">{t('keysec.badgeModelsHint', { list: models.join(', ') })}</TooltipContent>
        </Tooltip>
      )}
    </>
  )
}
