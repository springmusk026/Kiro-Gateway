import { ClipboardCopy, Download, Loader2, Search, ShieldAlert } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { copyToClipboard } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { accountApi } from '@/lib/queries.accounts'
import type { Account } from '@/lib/types'
import { HealthDot, PlanBadge } from './account-bits'
import { useAccountActions } from './actions'
import { authLabel, credentialsFromExport, downloadJson, errMsg, matchesSearch } from './lib'

export function ExportDialog({
  open,
  onOpenChange,
  accounts,
  filteredIds,
  initialIds,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  accounts: Account[]
  filteredIds: string[]
  initialIds?: string[]
}) {
  const { t } = useI18n()
  const act = useAccountActions()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState<'copy' | 'download' | null>(null)

  useEffect(() => {
    if (open) {
      setSelected(new Set(initialIds?.length ? initialIds : accounts.map((a) => a.id)))
      setQ('')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const shown = useMemo(() => accounts.filter((a) => matchesSearch(a, q)), [accounts, q])
  const ids = accounts.filter((a) => selected.has(a.id)).map((a) => a.id)

  const run = async (kind: 'copy' | 'download') => {
    if (!ids.length) {
      toast.warning(t('accounts.export.noSelection'))
      return
    }
    setBusy(kind)
    try {
      const data = await accountApi.export(ids)
      if (kind === 'download') {
        downloadJson(data, `kiro-accounts-${new Date().toISOString().slice(0, 10)}.json`)
        toast.success(t('accounts.export.downloaded', { n: data.accounts.length }))
      } else {
        await copyToClipboard(JSON.stringify(data.accounts.map(credentialsFromExport), null, 2))
        toast.success(t('accounts.export.copied', { n: data.accounts.length }))
      }
    } catch (e) {
      toast.error(errMsg(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('accounts.export.title')}</DialogTitle>
          <DialogDescription>{t('accounts.export.description')}</DialogDescription>
        </DialogHeader>
        <Alert className="border-warning/40 bg-warning/8">
          <ShieldAlert className="text-warning" />
          <AlertTitle>{t('accounts.export.secretsTitle')}</AlertTitle>
          <AlertDescription>{t('accounts.export.secretsText')}</AlertDescription>
        </Alert>
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative min-w-40 flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('common.search')} className="h-8 pl-8" />
            </div>
            <Button size="sm" variant="ghost" onClick={() => setSelected(new Set(accounts.map((a) => a.id)))}>
              {t('common.all')}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
              {t('common.none')}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setSelected(new Set(filteredIds))} disabled={!filteredIds.length}>
              {t('accounts.export.matchFilter')}
            </Button>
          </div>
          <div className="max-h-72 divide-y overflow-y-auto rounded-lg border">
            {shown.map((a) => (
              <label key={a.id} className="flex cursor-pointer items-center gap-3 px-3 py-2 text-sm hover:bg-muted/50">
                <Checkbox
                  checked={selected.has(a.id)}
                  onCheckedChange={(v) =>
                    setSelected((s) => {
                      const n = new Set(s)
                      if (v === true) n.add(a.id)
                      else n.delete(a.id)
                      return n
                    })
                  }
                />
                <HealthDot account={a} />
                <span className="min-w-0 flex-1 truncate">{act.name(a)}</span>
                <span className="hidden text-xs text-muted-foreground sm:inline">{authLabel(a, t)}</span>
                <PlanBadge account={a} />
              </label>
            ))}
            {!shown.length && <p className="p-4 text-center text-sm text-muted-foreground">{t('accounts.empty.noMatch')}</p>}
          </div>
          <p className="text-xs text-muted-foreground">{t('accounts.export.selected', { n: ids.length, total: accounts.length })}</p>
        </div>
        <DialogFooter className="gap-2 sm:justify-between">
          <p className="self-center text-xs text-muted-foreground">{t('accounts.export.copyHint')}</p>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => run('copy')} disabled={!!busy || !ids.length}>
              {busy === 'copy' ? <Loader2 className="animate-spin" /> : <ClipboardCopy />} {t('accounts.export.copy')}
            </Button>
            <Button onClick={() => run('download')} disabled={!!busy || !ids.length}>
              {busy === 'download' ? <Loader2 className="animate-spin" /> : <Download />} {t('accounts.export.download')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
