import { ArrowLeft, Loader2, Upload } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import { errorInfo, type ErrorInfo } from '../lib'
import { ImportResults, type ImportRow } from './results'
import { Code, ErrorCallout, FlowFooter, HelpSteps, RegionField, type FlowProps } from './shared'

export function SsoTokenFlow({ onBack, onAdded, onClose, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [tokens, setTokens] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)
  const [rows, setRows] = useState<ImportRow[] | null>(null)
  const count = tokens.split(/\r?\n/).filter((l) => l.trim()).length

  const submit = async () => {
    if (!count) {
      setError({ message: t('accounts.sso.missing') })
      return
    }
    setBusy(true)
    setError(null)
    try {
      const r = await authApi.ssoToken(tokens.trim(), region.trim() || 'us-east-1')
      const accounts = r.accounts ?? []
      const errors = r.errors ?? []
      if (!errors.length) {
        onSuccess(accounts, t('accounts.sso.success', { n: accounts.length }))
        return
      }
      onAdded(accounts)
      setRows([
        ...accounts.map((a) => ({ label: a.email || a.id, ok: true })),
        ...errors.map((e) => ({ label: t('accounts.sso.tokenFailed'), ok: false, error: { message: e } })),
      ])
    } catch (e) {
      const info = errorInfo(e)
      // All tokens failed: the server joins messages with "; ".
      if (info.status === 500 && count > 1 && info.message.includes('; ')) {
        setRows(info.message.split('; ').map((m) => ({ label: t('accounts.sso.tokenFailed'), ok: false, error: { message: m } })))
      } else setError(info)
    } finally {
      setBusy(false)
    }
  }

  if (rows) return <ImportResults rows={rows} onClose={onClose} onAgain={() => setRows(null)} />

  return (
    <div className="space-y-4">
      <HelpSteps
        title={t('accounts.sso.howTo')}
        steps={[
          <>
            {t('accounts.sso.step1')} <Code>view.awsapps.com/start</Code>
          </>,
          t('accounts.sso.step2'),
          <>
            {t('accounts.sso.step3')} <Code>x-amz-sso_authn</Code>
          </>,
        ]}
      />
      <div className="space-y-1.5">
        <div className="flex items-baseline justify-between">
          <Label htmlFor="sso-tokens">x-amz-sso_authn</Label>
          <span className="text-xs text-muted-foreground tabular">{t('accounts.sso.count', { n: count })}</span>
        </div>
        <Textarea
          id="sso-tokens"
          value={tokens}
          onChange={(e) => setTokens(e.target.value)}
          rows={5}
          spellCheck={false}
          placeholder={t('accounts.sso.placeholder')}
          className="max-h-64 font-mono text-xs"
        />
        <p className="text-xs text-muted-foreground">{t('accounts.sso.bulkHint')}</p>
      </div>
      <RegionField id="sso-region" value={region} onChange={setRegion} />
      <ErrorCallout error={error} />
      <FlowFooter>
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button onClick={submit} disabled={busy || !count}>
          {busy ? <Loader2 className="animate-spin" /> : <Upload />} {t('accounts.sso.import', { n: count })}
        </Button>
      </FlowFooter>
    </div>
  )
}
