import { Link } from '@tanstack/react-router'
import { AlertTriangle, Eye, EyeOff, KeyRound, LockKeyhole, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { setPassword } from '@/lib/api'
import { useI18n } from '@/lib/i18n'
import { useStatus } from '@/lib/queries'
import { useApiKeys, useSaveSettings, useSettings } from '@/lib/queries.settings'
import { cn } from '@/lib/utils'
import { SectionCard, SettingRow, runSave, useDraft } from './section-card'

export function ApiAccessCard() {
  const { t } = useI18n()
  const confirm = useConfirm()
  const settings = useSettings()
  const status = useStatus()
  const keys = useApiKeys()
  const save = useSaveSettings()
  const server = settings.data ? { requireApiKey: settings.data.requireApiKey } : undefined
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  const enabledKeys = (keys.data ?? []).filter((k) => k.enabled).length

  const onSave = async () => {
    if (!draft) return
    if (draft.requireApiKey && keys.data && enabledKeys === 0) {
      const ok = await confirm({
        title: t('settings.requireNoKeysTitle'),
        description: t('settings.requireNoKeysBody'),
        confirmLabel: t('settings.requireAnyway'),
        destructive: true,
      })
      if (!ok) return
    }
    if (await runSave(() => save.mutateAsync({ requireApiKey: draft.requireApiKey }), t('settings.saved'))) commit()
  }

  return (
    <SectionCard
      icon={ShieldCheck}
      title={t('settings.apiAccess')}
      description={t('settings.apiAccessDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      onSave={onSave}
      onReset={reset}
      footerExtra={
        <Link to="/keys" className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
          {t('settings.manageKeys', { n: enabledKeys })}
        </Link>
      }
    >
      {draft && (
        <div className="space-y-4">
          {status.data?.apiOpen && !draft.requireApiKey && (
            <Alert className="border-warning/40 bg-warning/10">
              <AlertTriangle className="text-warning" />
              <AlertTitle>{t('settings.apiOpenTitle')}</AlertTitle>
              <AlertDescription>{t('settings.apiOpenBody')}</AlertDescription>
            </Alert>
          )}
          <SettingRow
            id="require-key"
            label={t('settings.requireApiKey')}
            hint={draft.requireApiKey ? t('settings.requireOnHint') : t('settings.requireOffHint')}
          >
            <Switch
              id="require-key"
              checked={draft.requireApiKey}
              onCheckedChange={(v) => setDraft((d) => ({ ...d, requireApiKey: v }))}
            />
          </SettingRow>
          <p className="rounded-lg bg-muted/50 px-3 py-2 font-mono text-xs text-muted-foreground">
            Authorization: Bearer &lt;key&gt; · x-api-key: &lt;key&gt;
          </p>
        </div>
      )}
    </SectionCard>
  )
}

function strength(pw: string): 0 | 1 | 2 | 3 | 4 {
  if (!pw) return 0
  let s = 0
  if (pw.length >= 8) s++
  if (pw.length >= 14) s++
  const classes = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((r) => r.test(pw)).length
  if (classes >= 3) s++
  if (classes === 4 && pw.length >= 12) s++
  if (pw.trim().length < 8 || pw === 'changeme') return 1
  return Math.max(1, Math.min(4, s)) as 1 | 2 | 3 | 4
}

export function PasswordCard() {
  const { t } = useI18n()
  const save = useSaveSettings()
  const [pw, setPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [show, setShow] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)
  const s = strength(pw)
  const tooShort = pw.length > 0 && pw.trim().length < 8
  const isDefault = pw === 'changeme'
  const mismatch = confirmPw.length > 0 && pw !== confirmPw
  const valid = pw.length > 0 && !tooShort && !isDefault && pw === confirmPw
  const labels = ['', t('settings.pwWeak'), t('settings.pwFair'), t('settings.pwGood'), t('settings.pwStrong')]

  const onSave = async () => {
    setServerError(null)
    try {
      await save.mutateAsync({ password: pw })
    } catch (e) {
      setServerError(e instanceof Error ? e.message : String(e))
      return
    }
    // Keep this session signed in with the new password.
    let remember = false
    try {
      remember = localStorage.getItem('kiro_remember') === '1'
    } catch {
      /* storage unavailable */
    }
    setPassword(pw, remember)
    setPw('')
    setConfirmPw('')
    toast.success(t('settings.pwChanged'))
  }

  return (
    <SectionCard
      icon={LockKeyhole}
      title={t('settings.password')}
      description={t('settings.passwordDesc')}
      dirty={pw.length > 0 || confirmPw.length > 0}
      saving={save.isPending}
      saveDisabled={!valid}
      onSave={onSave}
      onReset={() => {
        setPw('')
        setConfirmPw('')
        setServerError(null)
      }}
      error={serverError}
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (valid) onSave()
        }}
      >
        <input type="text" name="username" autoComplete="username" value="admin" readOnly hidden />
        <div className="space-y-2">
          <Label htmlFor="new-pw">{t('settings.newPassword')}</Label>
          <div className="relative">
            <Input
              id="new-pw"
              type={show ? 'text' : 'password'}
              autoComplete="new-password"
              value={pw}
              onChange={(e) => {
                setPw(e.target.value)
                setServerError(null)
              }}
              aria-invalid={tooShort || isDefault}
              className="pr-10"
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="absolute right-0.5 top-1/2 size-8 -translate-y-1/2"
              onClick={() => setShow((v) => !v)}
              aria-label={show ? t('settings.hidePassword') : t('settings.showPassword')}
            >
              {show ? <EyeOff /> : <Eye />}
            </Button>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex flex-1 gap-1" aria-hidden>
              {[1, 2, 3, 4].map((i) => (
                <span
                  key={i}
                  className={cn(
                    'h-1 flex-1 rounded-full bg-muted transition-colors',
                    s >= i && (s <= 1 ? 'bg-destructive' : s === 2 ? 'bg-warning' : 'bg-success'),
                  )}
                />
              ))}
            </div>
            <span className="w-14 text-right text-xs text-muted-foreground">{labels[s]}</span>
          </div>
          {(tooShort || isDefault) && (
            <p className="text-xs text-destructive">{isDefault ? t('settings.pwDefault') : t('settings.pwMin')}</p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="confirm-pw">{t('settings.confirmPassword')}</Label>
          <Input
            id="confirm-pw"
            type={show ? 'text' : 'password'}
            autoComplete="new-password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            aria-invalid={mismatch}
          />
          {mismatch && <p className="text-xs text-destructive">{t('settings.pwMismatch')}</p>}
        </div>
        <p className="flex items-start gap-2 text-xs text-muted-foreground sm:col-span-2">
          <KeyRound className="mt-0.5 size-3.5 shrink-0" />
          {t('settings.pwEnvNote')}
        </p>
      </form>
    </SectionCard>
  )
}
