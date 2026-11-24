import { useQueryClient } from '@tanstack/react-query'
import { Download, Loader2, Power, PowerOff, RefreshCw, RotateCcw, Trash2, X } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useI18n } from '@/lib/i18n'
import { qk } from '@/lib/queries'
import { accountApi, invalidateAccounts } from '@/lib/queries.accounts'
import { errMsg } from './lib'

type Op = 'enable' | 'disable' | 'refresh' | 'models' | 'delete'

export function BatchBar({ ids, onClear, onExport }: { ids: string[]; onClear: () => void; onExport: () => void }) {
  const { t } = useI18n()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [running, setRunning] = useState<Op | null>(null)
  const n = ids.length

  const sequential = async (fn: (id: string) => Promise<unknown>, label: (done: number) => string) => {
    const tid = toast.loading(label(0))
    let ok = 0
    let fail = 0
    for (const id of ids) {
      try {
        await fn(id)
        ok++
      } catch {
        fail++
      }
      toast.loading(label(ok + fail), { id: tid })
    }
    return { ok, fail, tid }
  }

  const run = async (op: Op) => {
    const titles: Record<Op, string> = {
      enable: t('accounts.batch.confirmEnable', { n }),
      disable: t('accounts.batch.confirmDisable', { n }),
      refresh: t('accounts.batch.confirmRefresh', { n }),
      models: t('accounts.batch.confirmModels', { n }),
      delete: t('accounts.batch.confirmDelete', { n }),
    }
    const descs: Partial<Record<Op, string>> = {
      enable: t('accounts.batch.enableHint'),
      refresh: t('accounts.batch.refreshHint'),
      delete: t('accounts.batch.deleteHint'),
    }
    const ok = await confirm({
      title: titles[op],
      description: descs[op],
      destructive: op === 'delete' || op === 'disable',
      confirmLabel: op === 'delete' ? t('common.delete') : undefined,
    })
    if (!ok) return
    setRunning(op)
    try {
      if (op === 'enable' || op === 'disable') {
        await accountApi.batch(ids, op)
        toast.success(t(op === 'enable' ? 'accounts.batch.enabled' : 'accounts.batch.disabled', { n }))
      } else if (op === 'refresh') {
        const tid = toast.loading(t('accounts.batch.refreshing', { n }))
        try {
          const r = await accountApi.batch<{ refreshed: number; failed: number }>(ids, 'refresh')
          const msg = t('accounts.batch.refreshResult', { ok: r.refreshed, fail: r.failed })
          if (r.failed > 0) toast.warning(msg, { id: tid })
          else toast.success(msg, { id: tid })
        } catch (e) {
          toast.error(errMsg(e), { id: tid })
        }
      } else if (op === 'models') {
        const r = await sequential(accountApi.refreshModels, (d) => t('accounts.batch.progress', { done: d, n }))
        const msg = t('accounts.batch.modelsResult', { ok: r.ok, fail: r.fail })
        if (r.fail) toast.warning(msg, { id: r.tid })
        else toast.success(msg, { id: r.tid })
        qc.invalidateQueries({ queryKey: qk.models })
      } else if (op === 'delete') {
        const r = await sequential(accountApi.remove, (d) => t('accounts.batch.progress', { done: d, n }))
        const msg = t('accounts.batch.deleteResult', { ok: r.ok, fail: r.fail })
        if (r.fail) toast.warning(msg, { id: r.tid })
        else toast.success(msg, { id: r.tid })
      }
      onClear()
    } catch (e) {
      toast.error(errMsg(e))
    } finally {
      setRunning(null)
      await invalidateAccounts(qc)
    }
  }

  if (!n) return null
  const btn = (op: Op, icon: ReactNode, label: string, extra?: string) => (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="sm" variant="ghost" className={extra} onClick={() => run(op)} disabled={!!running} aria-label={label}>
          {running === op ? <Loader2 className="animate-spin" /> : icon}
          <span className="hidden md:inline">{label}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent className="md:hidden">{label}</TooltipContent>
    </Tooltip>
  )

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-40 flex justify-center px-4 lg:pl-[256px]">
      <div
        role="toolbar"
        aria-label={t('accounts.batch.toolbar')}
        className="pointer-events-auto flex max-w-full items-center gap-1 overflow-x-auto rounded-2xl border bg-popover/95 p-1.5 shadow-lg backdrop-blur-md animate-rise"
      >
        <span className="flex shrink-0 items-center gap-2 px-2 text-sm font-medium">
          <span className="grid h-6 min-w-6 place-items-center rounded-full bg-primary px-1.5 text-xs text-primary-foreground tabular">{n}</span>
          <span className="hidden sm:inline">{t('accounts.batch.selected')}</span>
        </span>
        <Separator orientation="vertical" className="mx-1 h-6" />
        {btn('enable', <Power />, t('common.enable'))}
        {btn('disable', <PowerOff />, t('common.disable'))}
        {btn('refresh', <RefreshCw />, t('accounts.batch.refresh'))}
        {btn('models', <RotateCcw />, t('accounts.batch.models'))}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="sm" variant="ghost" onClick={onExport} disabled={!!running} aria-label={t('accounts.export.button')}>
              <Download />
              <span className="hidden md:inline">{t('accounts.export.button')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent className="md:hidden">{t('accounts.export.button')}</TooltipContent>
        </Tooltip>
        {btn('delete', <Trash2 />, t('common.delete'), 'text-destructive hover:bg-destructive/10 hover:text-destructive')}
        <Separator orientation="vertical" className="mx-1 h-6" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon" variant="ghost" className="size-8" onClick={onClear} aria-label={t('accounts.batch.clear')}>
              <X />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('accounts.batch.clear')}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}
