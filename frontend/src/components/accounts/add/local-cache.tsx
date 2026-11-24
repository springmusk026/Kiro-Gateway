import { ArrowLeft, Loader2, Upload } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import { errorInfo, type ErrorInfo } from '../lib'
import { Code, ErrorCallout, FileTextarea, FlowFooter, type FlowProps } from './shared'

type Provider = 'BuilderId' | 'Enterprise' | 'Google' | 'Github'
const PROVIDERS: Provider[] = ['BuilderId', 'Enterprise', 'Google', 'Github']

export function LocalCacheFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [provider, setProvider] = useState<Provider>('BuilderId')
  const [tokenJson, setTokenJson] = useState('')
  const [clientJson, setClientJson] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)
  const social = provider === 'Google' || provider === 'Github'

  const submit = async () => {
    setError(null)
    if (!tokenJson.trim()) return setError({ message: t('accounts.local.tokenMissing') })
    let token: Record<string, string>
    try {
      token = JSON.parse(tokenJson)
    } catch {
      return setError({ message: t('accounts.local.tokenInvalid') })
    }
    if (!token?.refreshToken) return setError({ message: t('accounts.local.refreshTokenMissing') })
    let client: Record<string, string> | null = null
    if (!social) {
      if (!clientJson.trim()) return setError({ message: t('accounts.local.clientMissing') })
      try {
        client = JSON.parse(clientJson)
      } catch {
        return setError({ message: t('accounts.local.clientInvalid') })
      }
      if (!client?.clientId || !client?.clientSecret) return setError({ message: t('accounts.local.clientSecretMissing') })
    }
    setBusy(true)
    try {
      const r = await authApi.credentials({
        refreshToken: token.refreshToken,
        accessToken: token.accessToken || '',
        clientId: client?.clientId || '',
        clientSecret: client?.clientSecret || '',
        region: token.region || '',
        authMethod: client ? 'idc' : 'social',
        provider,
      })
      onSuccess([r.account], t('accounts.local.success'))
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
        <p className="font-medium text-foreground">{t('accounts.local.location')}</p>
        <p className="mt-1">
          Windows: <Code>%USERPROFILE%\.aws\sso\cache\</Code>
        </p>
        <p className="mt-1">
          macOS / Linux: <Code>~/.aws/sso/cache/</Code>
        </p>
      </div>
      <div className="space-y-1.5">
        <Label>{t('accounts.local.provider')}</Label>
        <ToggleGroup
          type="single"
          variant="outline"
          value={provider}
          onValueChange={(v) => v && setProvider(v as Provider)}
          className="flex w-full flex-wrap"
        >
          {PROVIDERS.map((p) => (
            <ToggleGroupItem key={p} value={p} className="flex-1 px-3 text-xs">
              {t(`accounts.local.provider${p}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      <FileTextarea
        id="local-token"
        label={<span className="font-mono">kiro-auth-token.json</span>}
        value={tokenJson}
        onChange={setTokenJson}
        placeholder='{"accessToken": "...", "refreshToken": "...", "region": "us-east-1"}'
        required
      />
      {!social && (
        <FileTextarea
          id="local-client"
          label={<span className="font-mono">{'{hash}.json'}</span>}
          hint={t('accounts.local.clientHint')}
          value={clientJson}
          onChange={setClientJson}
          placeholder='{"clientId": "...", "clientSecret": "..."}'
          required
        />
      )}
      <ErrorCallout error={error} />
      <FlowFooter>
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <Button onClick={submit} disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <Upload />} {t('accounts.add.import')}
        </Button>
      </FlowFooter>
    </div>
  )
}
