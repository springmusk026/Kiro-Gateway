import { ArrowLeft, ArrowRight, Check, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import { errorInfo, type ErrorInfo } from '../lib'
import { ErrorCallout, FlowFooter, RegionField, UrlBox, type FlowProps } from './shared'

export function IamFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [startUrl, setStartUrl] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [session, setSession] = useState<{ id: string; url: string } | null>(null)
  const [callback, setCallback] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)

  const start = async () => {
    if (!startUrl.trim()) {
      setError({ message: t('accounts.iam.startUrlRequired') })
      return
    }
    setBusy(true)
    setError(null)
    try {
      const r = await authApi.iamStart(startUrl.trim(), region.trim() || 'us-east-1')
      setSession({ id: r.sessionId, url: r.authorizeUrl })
      setCallback('')
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setBusy(false)
    }
  }

  const complete = async () => {
    if (!session) return
    if (!callback.trim()) {
      setError({ message: t('accounts.iam.callbackRequired') })
      return
    }
    setBusy(true)
    setError(null)
    try {
      const r = await authApi.iamComplete(session.id, callback.trim())
      onSuccess([r.account], t('accounts.iam.success'))
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setBusy(false)
    }
  }

  if (!session) {
    return (
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          start()
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="iam-url">{t('accounts.iam.startUrl')}</Label>
          <Input
            id="iam-url"
            value={startUrl}
            onChange={(e) => setStartUrl(e.target.value)}
            placeholder="https://xxx.awsapps.com/start"
            className="font-mono text-sm"
            autoFocus
          />
        </div>
        <RegionField id="iam-region" value={region} onChange={setRegion} />
        <ErrorCallout error={error} />
        <FlowFooter>
          <Button type="button" variant="ghost" onClick={onBack}>
            <ArrowLeft /> {t('common.back')}
          </Button>
          <Button type="submit" disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : <ArrowRight />} {t('accounts.iam.getLink')}
          </Button>
        </FlowFooter>
      </form>
    )
  }

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        complete()
      }}
    >
      <UrlBox url={session.url} label={t('accounts.iam.loginUrl')} />
      <p className="text-sm text-muted-foreground">{t('accounts.iam.instructions')}</p>
      <div className="space-y-1.5">
        <Label htmlFor="iam-cb">{t('accounts.iam.callbackUrl')}</Label>
        <Textarea
          id="iam-cb"
          value={callback}
          onChange={(e) => setCallback(e.target.value)}
          placeholder="http://127.0.0.1/oauth/callback?code=...&state=..."
          rows={3}
          className="font-mono text-xs"
          spellCheck={false}
        />
      </div>
      <ErrorCallout error={error} />
      <FlowFooter>
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            // Reset the PKCE session so a new Start URL / region gets a fresh one.
            setSession(null)
            setCallback('')
            setError(null)
          }}
          disabled={busy}
        >
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <Check />} {t('accounts.iam.complete')}
        </Button>
      </FlowFooter>
    </form>
  )
}
