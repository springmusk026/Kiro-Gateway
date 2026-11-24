import { ArrowLeft, FileJson, Loader2, Rows3, Upload } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import type { NewAccountRef } from '@/lib/types'
import { credentialPayload, errorInfo, parseCredentialsInput } from '../lib'
import { UsageBar } from '../account-bits'
import { ImportResults, type ImportRow } from './results'
import { FileTextarea, FlowFooter, type FlowProps } from './shared'

export function CredentialsFlow({ onBack, onAdded, onClose, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [raw, setRaw] = useState('')
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  const [result, setResult] = useState<{ rows: ImportRow[]; skipped: number } | null>(null)
  const alive = useRef(true)
  const parsed = useMemo(() => parseCredentialsInput(raw), [raw])
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const submit = async () => {
    if (!parsed.ok) return
    const items = parsed.items
    const rows: ImportRow[] = []
    const added: NewAccountRef[] = []
    setProgress({ done: 0, total: items.length })
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      const label = item.email || item.nickname || item.id || t('accounts.credentials.item', { n: i + 1 })
      const payload = credentialPayload(item)
      if (!payload) {
        rows.push({ label, ok: false, error: { message: t('accounts.credentials.noToken') } })
      } else {
        try {
          const r = await authApi.credentials(payload)
          added.push(r.account)
          rows.push({ label: r.account.email || label || r.account.id, ok: true })
        } catch (e) {
          rows.push({ label, ok: false, error: errorInfo(e) })
        }
      }
      if (!alive.current) return
      setProgress({ done: i + 1, total: items.length })
    }
    setProgress(null)
    const failed = rows.filter((r) => !r.ok).length
    if (!failed && !parsed.skipped) {
      onSuccess(added, t('accounts.credentials.success', { n: added.length }))
      return
    }
    if (added.length) onAdded(added)
    setResult({ rows, skipped: parsed.skipped })
  }

  if (result)
    return (
      <ImportResults
        rows={result.rows}
        skipped={result.skipped}
        onClose={onClose}
        onAgain={() => {
          setResult(null)
        }}
      />
    )

  const busy = !!progress
  return (
    <div className="space-y-4">
      <FileTextarea
        id="cred-json"
        label={t('accounts.credentials.label')}
        value={raw}
        onChange={setRaw}
        rows={8}
        accept=".json,.txt,application/json,text/plain"
        placeholder={'{"refreshToken": "...", "clientId": "...", "clientSecret": "...", "authMethod": "idc"}\n\n— or —\n\nemail----password----refreshToken----clientId----clientSecret'}
        hint={t('accounts.credentials.hint')}
      />
      {raw.trim() && (
        <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm animate-rise" aria-live="polite">
          {parsed.ok ? (
            <>
              {parsed.format === 'json' ? <FileJson className="size-4 text-primary" /> : <Rows3 className="size-4 text-primary" />}
              <span>
                {t(parsed.format === 'json' ? 'accounts.credentials.detectedJson' : 'accounts.credentials.detectedLines', {
                  n: parsed.items.length,
                })}
                {parsed.skipped > 0 && <span className="text-warning"> · {t('accounts.credentials.skipped', { n: parsed.skipped })}</span>}
              </span>
            </>
          ) : (
            <span className="text-destructive">
              {parsed.reason === 'allSkipped'
                ? t('accounts.credentials.allSkipped', { n: parsed.skipped })
                : t('accounts.credentials.invalid')}
            </span>
          )}
        </div>
      )}
      {progress && (
        <div className="space-y-1.5">
          <div className="flex justify-between text-xs text-muted-foreground">
            <span>{t('accounts.credentials.importing')}</span>
            <span className="tabular">
              {progress.done} / {progress.total}
            </span>
          </div>
          <UsageBar fraction={progress.done / progress.total} className="[&>div]:bg-primary" />
        </div>
      )}
      <FlowFooter>
        <Button
          variant="ghost"
          onClick={() => {
            alive.current = false
            onBack()
          }}
          disabled={busy}
        >
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button onClick={submit} disabled={busy || !parsed.ok}>
          {busy ? <Loader2 className="animate-spin" /> : <Upload />}{' '}
          {t('accounts.credentials.import', { n: parsed.ok ? parsed.items.length : 0 })}
        </Button>
      </FlowFooter>
    </div>
  )
}
