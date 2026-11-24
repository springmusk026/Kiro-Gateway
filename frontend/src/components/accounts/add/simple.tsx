import { ArrowLeft, Loader2, Upload } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import { errorInfo, type ErrorInfo } from '../lib'
import { Code, ErrorCallout, FlowFooter, HelpSteps, RegionField, type FlowProps } from './shared'

/** Social login (Google / GitHub) via the RefreshToken cookie from app.kiro.dev. */
export function SocialTokenFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [provider, setProvider] = useState<'Google' | 'Github'>('Google')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)

  const submit = async () => {
    if (!token.trim()) return setError({ message: t('accounts.social.missing') })
    setBusy(true)
    setError(null)
    try {
      const r = await authApi.credentials({
        refreshToken: token.trim(),
        accessToken: '',
        clientId: '',
        clientSecret: '',
        authMethod: 'social',
        provider,
      })
      onSuccess([r.account], t('accounts.social.success'))
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <HelpSteps
        title={t('accounts.social.howTo')}
        steps={[
          <>
            {t('accounts.social.step1')}{' '}
            <a href="https://app.kiro.dev/account/usage" target="_blank" rel="noopener noreferrer" className="text-primary underline-offset-4 hover:underline">
              app.kiro.dev
            </a>
          </>,
          <>
            {t('accounts.social.step2')} <Code>https://app.kiro.dev</Code>
          </>,
          <>
            {t('accounts.social.step3')} <Code>RefreshToken</Code>
          </>,
        ]}
      />
      <div className="space-y-1.5">
        <Label>{t('accounts.social.provider')}</Label>
        <ToggleGroup type="single" variant="outline" value={provider} onValueChange={(v) => v && setProvider(v as 'Google' | 'Github')} className="w-full">
          <ToggleGroupItem value="Google" className="flex-1">
            Google
          </ToggleGroupItem>
          <ToggleGroupItem value="Github" className="flex-1">
            GitHub
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="social-token">RefreshToken</Label>
        <Textarea
          id="social-token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          rows={4}
          spellCheck={false}
          className="font-mono text-xs"
          placeholder={t('accounts.social.placeholder')}
        />
      </div>
      <ErrorCallout error={error} />
      <FlowFooter>
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button onClick={submit} disabled={busy || !token.trim()}>
          {busy ? <Loader2 className="animate-spin" /> : <Upload />} {t('accounts.add.import')}
        </Button>
      </FlowFooter>
    </div>
  )
}

/** Kiro API key: `ksk_…` or `ksk_…|region` (region in the key wins). */
export function ApiKeyFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [key, setKey] = useState('')
  const [region, setRegion] = useState('us-east-1')
  const [nickname, setNickname] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)
  const [rawKey, keyRegion] = key.trim().split('|').map((s) => s.trim())
  const looksWrong = !!rawKey && !rawKey.startsWith('ksk_')

  const submit = async () => {
    if (!rawKey) return setError({ message: t('accounts.apikey.missing') })
    setBusy(true)
    setError(null)
    try {
      const r = await authApi.credentials({
        kiroApiKey: rawKey,
        authMethod: 'api_key',
        region: keyRegion || region.trim() || 'us-east-1',
        nickname: nickname.trim(),
      })
      onSuccess([r.account], t('accounts.apikey.success'))
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <div className="space-y-1.5">
        <Label htmlFor="ak-key">{t('accounts.apikey.label')}</Label>
        <Input
          id="ak-key"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="ksk_xxxxxxxx  or  ksk_xxxxxxxx|eu-central-1"
          className="font-mono text-sm"
          autoComplete="off"
          spellCheck={false}
          autoFocus
          aria-invalid={looksWrong}
        />
        <p className={looksWrong ? 'text-xs text-warning' : 'text-xs text-muted-foreground'}>
          {looksWrong ? t('accounts.apikey.prefixWarning') : t('accounts.apikey.hint')}
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className={keyRegion ? 'opacity-60' : undefined}>
          <RegionField
            id="ak-region"
            value={keyRegion || region}
            onChange={setRegion}
            hint={keyRegion ? t('accounts.apikey.regionFromKey') : undefined}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ak-nick">
            {t('accounts.detail.nickname')} <span className="text-muted-foreground">({t('common.optional')})</span>
          </Label>
          <Input id="ak-nick" value={nickname} onChange={(e) => setNickname(e.target.value)} />
        </div>
      </div>
      <ErrorCallout error={error} />
      <FlowFooter>
        <Button type="button" variant="ghost" onClick={onBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button type="submit" disabled={busy || !rawKey}>
          {busy ? <Loader2 className="animate-spin" /> : <Upload />} {t('accounts.add.import')}
        </Button>
      </FlowFooter>
    </form>
  )
}
