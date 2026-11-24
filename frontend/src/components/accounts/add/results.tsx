import { CheckCircle2, XCircle } from 'lucide-react'
import { CopyButton } from '@/components/app/copy-button'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/lib/i18n'
import type { ErrorInfo } from '../lib'
import { FlowFooter } from './shared'

export interface ImportRow {
  label: string
  ok: boolean
  error?: ErrorInfo
}

export function ImportResults({ rows, skipped = 0, onClose, onAgain }: { rows: ImportRow[]; skipped?: number; onClose: () => void; onAgain: () => void }) {
  const { t } = useI18n()
  const ok = rows.filter((r) => r.ok).length
  const fail = rows.length - ok
  return (
    <div className="space-y-4 animate-rise">
      <div className="grid grid-cols-3 gap-2 text-center">
        <div className="rounded-lg border border-success/25 bg-success/5 p-3">
          <p className="text-2xl font-semibold text-success tabular">{ok}</p>
          <p className="text-xs text-muted-foreground">{t('accounts.import.added')}</p>
        </div>
        <div className="rounded-lg border border-destructive/25 bg-destructive/5 p-3">
          <p className="text-2xl font-semibold text-destructive tabular">{fail}</p>
          <p className="text-xs text-muted-foreground">{t('accounts.import.failed')}</p>
        </div>
        <div className="rounded-lg border bg-muted/40 p-3">
          <p className="text-2xl font-semibold tabular">{skipped}</p>
          <p className="text-xs text-muted-foreground">{t('accounts.import.skipped')}</p>
        </div>
      </div>
      <ul className="max-h-72 divide-y overflow-y-auto rounded-lg border">
        {rows.map((r, i) => (
          <li key={i} className="flex items-start gap-2.5 px-3 py-2 text-sm">
            {r.ok ? <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" /> : <XCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
            <div className="min-w-0 flex-1">
              <p className="truncate">{r.label}</p>
              {r.error && <p className="break-words text-xs text-destructive">{r.error.message}</p>}
              {r.error?.hint && <p className="text-xs text-muted-foreground">{r.error.hint}</p>}
              {r.error?.rotatedRefreshToken && (
                <div className="mt-1 flex items-center gap-1 rounded border bg-muted/40 py-0.5 pl-2 pr-0.5">
                  <span className="shrink-0 text-[11px] text-muted-foreground">{t('accounts.add.rotatedShort')}</span>
                  <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{r.error.rotatedRefreshToken}</code>
                  <CopyButton value={r.error.rotatedRefreshToken} className="size-6" />
                </div>
              )}
            </div>
          </li>
        ))}
      </ul>
      <FlowFooter>
        <Button variant="ghost" onClick={onAgain}>
          {t('accounts.import.again')}
        </Button>
        <Button onClick={onClose}>{t('common.done')}</Button>
      </FlowFooter>
    </div>
  )
}
