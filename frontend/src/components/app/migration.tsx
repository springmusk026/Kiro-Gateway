import { AlertTriangle, ArrowRight, CheckCircle2, Database, FileJson, Loader2, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useMigrationStatus, useRunMigration, useStorageInfo } from '@/lib/queries.history'
import type { MigrationAction, MigrationStatus } from '@/lib/types'
import { cn } from '@/lib/utils'

const SNOOZE_KEY = 'kiro_migration_snoozed'

function bytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function Row({ label, json, db, missing }: { label: string; json: number; db?: number; missing: number }) {
  return (
    <tr className="border-b last:border-0">
      <td className="py-2 pr-3 text-muted-foreground">{label}</td>
      <td className="py-2 pr-3 text-right tabular">{json.toLocaleString()}</td>
      <td className="py-2 pr-3 text-right tabular">{db == null ? '—' : db.toLocaleString()}</td>
      <td className="py-2 text-right">
        {missing > 0 ? (
          <span className="font-medium text-warning tabular">{missing.toLocaleString()}</span>
        ) : (
          <CheckCircle2 className="ml-auto size-4 text-success" />
        )}
      </td>
    </tr>
  )
}

/** Comparison of config.json with the database, plus the actions to finish the move. */
export function MigrationPanel({ status, onDone, compact }: { status: MigrationStatus; onDone?: () => void; compact?: boolean }) {
  const { t, lang } = useI18n()
  const run = useRunMigration()
  const [deleteAfter, setDeleteAfter] = useState(true)
  const [confirmReplace, setConfirmReplace] = useState(false)
  const [pending, setPending] = useState<MigrationAction | null>(null)
  const s = status

  const go = (action: MigrationAction) => {
    setPending(action)
    run.mutate(
      { action, deleteAfter: action === 'delete' ? true : deleteAfter },
      {
        onSuccess: (r) => {
          const imported = r.importedAccounts + r.importedKeys + r.importedResponses
          if (r.deleted?.length) {
            toast.success(t('migration.doneDeleted'), {
              description: t('migration.doneDeletedHint', { n: r.deleted.length, imported }),
            })
            onDone?.()
          } else {
            toast.success(t('migration.doneImported', { a: r.importedAccounts, k: r.importedKeys, r: r.importedResponses }))
          }
        },
        onError: (e) => toast.error(t('migration.failed'), { description: e.message }),
        onSettled: () => {
          setPending(null)
          setConfirmReplace(false)
        },
      },
    )
  }

  const busy = run.isPending
  const missing = s.missingAccounts + s.missingKeys + s.missingResponses

  return (
    <div className="min-w-0 space-y-4">
      <div className="flex min-w-0 items-center gap-3 rounded-lg border bg-muted/30 p-3">
        <FileJson className="size-5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="font-mono text-xs break-all">{s.jsonPath}</p>
          <p className="text-xs text-muted-foreground">
            {s.jsonExists ? (
              <>
                {bytes(s.jsonSize)}
                {s.jsonModified && <> · {t('migration.modified', { ago: timeAgo(Date.parse(s.jsonModified) / 1000, lang) })}</>}
              </>
            ) : (
              t('migration.onlyResponses')
            )}
          </p>
        </div>
        <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
        <Database className="size-5 shrink-0 text-primary" />
      </div>

      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-xs text-muted-foreground">
            <th className="pb-2 text-left font-medium" />
            <th className="pb-2 pr-3 text-right font-medium">config.json</th>
            <th className="pb-2 pr-3 text-right font-medium">PostgreSQL</th>
            <th className="pb-2 text-right font-medium">{t('migration.missing')}</th>
          </tr>
        </thead>
        <tbody>
          <Row label={t('migration.accounts')} json={s.jsonAccounts} db={s.dbAccounts} missing={s.missingAccounts} />
          <Row label={t('migration.keys')} json={s.jsonKeys} db={s.dbKeys} missing={s.missingKeys} />
          <Row label={t('migration.responses')} json={s.jsonResponses} missing={s.missingResponses} />
        </tbody>
      </table>

      {s.state !== 'migrated' && (
        <label className="flex cursor-pointer items-start gap-2.5 text-sm">
          <Checkbox className="mt-0.5" checked={deleteAfter} onCheckedChange={(v) => setDeleteAfter(v === true)} />
          <span>
            {t('migration.deleteAfter')}
            <span className="block text-xs text-muted-foreground">{t('migration.deleteAfterHint')}</span>
          </span>
        </label>
      )}

      {s.state === 'diverged' && confirmReplace && (
        <div className="flex gap-2 rounded-lg border border-destructive/40 bg-destructive/[0.06] p-3 text-[13px]">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div className="space-y-2">
            <p>{t('migration.replaceWarn', { n: s.dbAccounts })}</p>
            <div className="flex gap-2">
              <Button size="sm" variant="destructive" disabled={busy} onClick={() => go('replace')}>
                {pending === 'replace' && <Loader2 className="size-3.5 animate-spin" />}
                {t('migration.replaceConfirm')}
              </Button>
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmReplace(false)}>
                {t('common.cancel')}
              </Button>
            </div>
          </div>
        </div>
      )}

      <div className={cn('flex flex-wrap gap-2', compact ? 'justify-start' : 'justify-end')}>
        {s.state === 'diverged' && !confirmReplace && (
          <Button variant="ghost" disabled={busy} onClick={() => setConfirmReplace(true)}>
            {t('migration.replace')}
          </Button>
        )}
        {s.state === 'migrated' ? (
          <Button variant="destructive" disabled={busy} onClick={() => go('delete')}>
            {pending === 'delete' ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
            {t('migration.deleteNow')}
          </Button>
        ) : (
          <Button disabled={busy} onClick={() => go('import')}>
            {pending === 'import' ? <Loader2 className="size-4 animate-spin" /> : <Database className="size-4" />}
            {s.state === 'pending' ? t('migration.migrate') : t('migration.importMissing', { n: missing })}
          </Button>
        )}
      </div>
    </div>
  )
}

export function migrationCopy(state: MigrationStatus['state']) {
  return {
    title: `migration.title.${state}` as 'migration.title.pending',
    text: `migration.text.${state}` as 'migration.text.pending',
  }
}

/** Prompt shown once per session when a config.json is still next to the database. */
export function MigrationPrompt() {
  const { t } = useI18n()
  const storage = useStorageInfo()
  const q = useMigrationStatus(!!storage.data?.database)
  const [open, setOpen] = useState(false)
  const s = q.data
  const actionable = !!s && s.state !== 'none'

  useEffect(() => {
    if (!actionable) return
    let snoozed = false
    try {
      snoozed = sessionStorage.getItem(SNOOZE_KEY) === s.state
    } catch {
      /* storage unavailable */
    }
    if (!snoozed) setOpen(true)
  }, [actionable, s?.state]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!s || !actionable) return null
  const copy = migrationCopy(s.state)
  const later = () => {
    try {
      sessionStorage.setItem(SNOOZE_KEY, s.state)
    } catch {
      /* storage unavailable */
    }
    setOpen(false)
  }

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? setOpen(true) : later())}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-lg [&>*]:min-w-0">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Database className="size-5 text-primary" />
            {t(copy.title)}
          </DialogTitle>
          <DialogDescription>{t(copy.text)}</DialogDescription>
        </DialogHeader>
        <MigrationPanel status={s} onDone={() => setOpen(false)} />
        <DialogFooter className="sm:justify-start">
          <Button variant="link" className="h-auto px-0 text-muted-foreground" onClick={later}>
            {t('migration.later')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Settings card content: status line when nothing is left, panel otherwise. */
export function MigrationSettingsBlock() {
  const { t } = useI18n()
  const q = useMigrationStatus()
  const s = q.data
  if (!s?.database) return null
  if (s.state === 'none') {
    return (
      <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
        <CheckCircle2 className="size-4 text-success" />
        {t('migration.clean')}
      </p>
    )
  }
  const copy = migrationCopy(s.state)
  return (
    <div className="space-y-3 rounded-xl border border-primary/25 bg-primary/[0.03] p-4">
      <div>
        <p className="flex items-center gap-2 font-medium">
          {t(copy.title)}
          <Badge variant="outline" className="font-normal">
            {t(`migration.badge.${s.state}` as 'migration.badge.pending')}
          </Badge>
        </p>
        <p className="mt-1 text-[13px] text-muted-foreground">{t(copy.text)}</p>
      </div>
      <MigrationPanel status={s} compact />
    </div>
  )
}
