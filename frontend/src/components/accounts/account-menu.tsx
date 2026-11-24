import {
  Boxes,
  ClipboardCopy,
  CreditCard,
  FlaskConical,
  Info,
  Loader2,
  MoreHorizontal,
  Power,
  PowerOff,
  RefreshCw,
  RotateCcw,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useI18n } from '@/lib/i18n'
import type { Account } from '@/lib/types'
import { useAccountActions } from './actions'
import { isBanned } from './lib'

export function AccountMenu({ account: a }: { account: Account }) {
  const { t } = useI18n()
  const act = useAccountActions()
  const busy = act.busy.has(a.id)
  const banned = isBanned(a)
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          aria-label={t('accounts.action.menu')}
          onClick={(e) => e.stopPropagation()}
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <MoreHorizontal className="size-4" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56" onClick={(e) => e.stopPropagation()}>
        <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">{act.name(a)}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => act.openDetail(a.id)}>
          <Info /> {t('accounts.action.details')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.refresh(a)} disabled={busy}>
          <RefreshCw /> {t('accounts.action.refresh')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.openTest(a.id)}>
          <FlaskConical /> {t('accounts.action.test')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.openDetail(a.id, 'models')}>
          <Boxes /> {t('accounts.action.models')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.refreshModels(a)} disabled={busy}>
          <RotateCcw /> {t('accounts.action.refreshModels')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.openDetail(a.id, 'settings')}>
          <CreditCard /> {t('accounts.action.overage')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => act.copyCredentials(a)} disabled={busy}>
          <ClipboardCopy /> {t('accounts.action.copyCredentials')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {banned ? (
          <DropdownMenuItem onSelect={() => act.reenable(a)} disabled={busy}>
            <Power /> {t('accounts.action.reenable')}
          </DropdownMenuItem>
        ) : a.enabled ? (
          <DropdownMenuItem onSelect={() => act.setEnabled(a, false)} disabled={busy}>
            <PowerOff /> {t('common.disable')}
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onSelect={() => act.setEnabled(a, true)} disabled={busy}>
            <Power /> {t('common.enable')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem variant="destructive" onSelect={() => act.remove(a)} disabled={busy}>
          <Trash2 /> {t('common.delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
