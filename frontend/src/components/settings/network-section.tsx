import { Eye, EyeOff, Globe } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { useProxyConfig, useSaveProxy } from '@/lib/queries.settings'
import { SectionCard, runSave, useDraft } from './section-card'

export type ProxyScheme = 'none' | 'http' | 'https' | 'socks5' | 'socks5h'
export interface ProxyParts {
  type: ProxyScheme
  host: string
  port: string
  user: string
  pass: string
}

const SCHEMES: { id: ProxyScheme; label: MessageKey }[] = [
  { id: 'none', label: 'settings.proxyNone' },
  { id: 'http', label: 'settings.proxyHttp' },
  { id: 'https', label: 'settings.proxyHttps' },
  { id: 'socks5', label: 'settings.proxySocks5' },
  { id: 'socks5h', label: 'settings.proxySocks5h' },
]
const DEFAULT_PORT: Record<ProxyScheme, string> = { none: '', http: '8080', https: '443', socks5: '1080', socks5h: '1080' }

const dec = (s: string) => {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

/** Parse a proxy URL without `new URL` (which mangles non-special schemes in some browsers). */
export function parseProxyURL(url: string): ProxyParts | null {
  const empty: ProxyParts = { type: 'none', host: '', port: '', user: '', pass: '' }
  if (!url.trim()) return empty
  const m = /^(https?|socks5h?):\/\/(?:([^@/]*)@)?(\[[^\]]+\]|[^:/?#]+)(?::(\d+))?\/?$/i.exec(url.trim())
  if (!m) return null
  const [, scheme, auth, host, port] = m
  let user = ''
  let pass = ''
  if (auth !== undefined) {
    const i = auth.indexOf(':')
    user = dec(i === -1 ? auth : auth.slice(0, i))
    pass = i === -1 ? '' : dec(auth.slice(i + 1))
  }
  return { type: scheme.toLowerCase() as ProxyScheme, host, port: port ?? '', user, pass }
}

export function buildProxyURL(p: ProxyParts, maskPassword = false): string {
  if (p.type === 'none') return ''
  let auth = ''
  if (p.user || p.pass) {
    auth = encodeURIComponent(p.user)
    if (p.pass) auth += ':' + (maskPassword ? '••••••' : encodeURIComponent(p.pass))
    auth += '@'
  }
  const host = p.host.trim()
  return `${p.type}://${auth}${host}${p.port ? ':' + p.port.trim() : ''}`
}

type Draft = { parts: ProxyParts; raw: string | null }

export function ProxyCard() {
  const { t } = useI18n()
  const q = useProxyConfig()
  const save = useSaveProxy()
  const [showPass, setShowPass] = useState(false)
  const server = useMemo<Draft | undefined>(() => {
    if (!q.data) return undefined
    const parts = parseProxyURL(q.data.proxyURL ?? '')
    return parts ? { parts, raw: null } : { parts: { type: 'none', host: '', port: '', user: '', pass: '' }, raw: q.data.proxyURL }
  }, [q.data])
  const { draft, setDraft, dirty, reset, commit } = useDraft<Draft>(server)

  const p = draft?.parts
  const hostErr = p && p.type !== 'none' && !p.host.trim() ? t('settings.proxyHostRequired') : null
  const portRequired = p?.type === 'socks5' || p?.type === 'socks5h'
  const portErr =
    p && p.type !== 'none' && (p.port.trim() ? +p.port < 1 || +p.port > 65535 : portRequired)
      ? t('settings.proxyPortInvalid')
      : null
  const hostBad = p && /[\s/@]/.test(p.host.trim()) ? t('settings.proxyHostInvalid') : null
  const invalid = draft?.raw == null && !!(hostErr || portErr || hostBad)
  const set = (patch: Partial<ProxyParts>) => setDraft((d) => ({ ...d, parts: { ...d.parts, ...patch } }))

  return (
    <SectionCard
      icon={Globe}
      title={t('settings.proxy')}
      description={t('settings.proxyDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={invalid}
      onReset={reset}
      onSave={async () => {
        if (!draft) return
        const url = draft.raw ?? buildProxyURL(draft.parts)
        if (await runSave(() => save.mutateAsync({ proxyURL: url }), t('settings.proxySaved'))) commit()
      }}
    >
      {draft && p && (
        <div className="space-y-4">
          {draft.raw != null && (
            <div className="space-y-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
              <p>{t('settings.proxyUnparsed')}</p>
              <code className="block break-all font-mono text-xs">{draft.raw.replace(/:[^:@/]*@/, ':••••••@')}</code>
              <Button type="button" size="sm" variant="outline" onClick={() => setDraft((d) => ({ ...d, raw: null }))}>
                {t('settings.proxyReplace')}
              </Button>
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-[180px_1fr_120px]">
            <div className="space-y-2">
              <Label htmlFor="px-type">{t('settings.proxyType')}</Label>
              <Select
                value={p.type}
                onValueChange={(v) =>
                  setDraft((d) => ({
                    raw: null,
                    parts: {
                      ...d.parts,
                      type: v as ProxyScheme,
                      port: d.parts.port || DEFAULT_PORT[v as ProxyScheme],
                    },
                  }))
                }
              >
                <SelectTrigger id="px-type" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SCHEMES.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {t(s.label)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {p.type !== 'none' && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="px-host">{t('settings.proxyHost')}</Label>
                  <Input
                    id="px-host"
                    className="font-mono"
                    placeholder="127.0.0.1"
                    value={p.host}
                    aria-invalid={!!(hostErr || hostBad)}
                    onChange={(e) => set({ host: e.target.value })}
                  />
                  {(hostErr || hostBad) && <p className="text-xs text-destructive">{hostErr || hostBad}</p>}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="px-port">{t('settings.proxyPort')}</Label>
                  <Input
                    id="px-port"
                    className="font-mono tabular"
                    inputMode="numeric"
                    value={p.port}
                    aria-invalid={!!portErr}
                    onChange={(e) => set({ port: e.target.value.replace(/[^\d]/g, '') })}
                  />
                  {portErr && <p className="text-xs text-destructive">{portErr}</p>}
                </div>
              </>
            )}
          </div>
          {p.type !== 'none' && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="px-user">
                  {t('settings.proxyUser')} <span className="font-normal text-muted-foreground">({t('common.optional')})</span>
                </Label>
                <Input id="px-user" autoComplete="off" value={p.user} onChange={(e) => set({ user: e.target.value })} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="px-pass">
                  {t('settings.proxyPass')} <span className="font-normal text-muted-foreground">({t('common.optional')})</span>
                </Label>
                <div className="relative">
                  <Input
                    id="px-pass"
                    type={showPass ? 'text' : 'password'}
                    autoComplete="new-password"
                    className="pr-10"
                    value={p.pass}
                    onChange={(e) => set({ pass: e.target.value })}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="absolute right-0.5 top-1/2 -translate-y-1/2"
                    onClick={() => setShowPass((v) => !v)}
                    aria-label={showPass ? t('settings.hidePassword') : t('settings.showPassword')}
                  >
                    {showPass ? <EyeOff /> : <Eye />}
                  </Button>
                </div>
              </div>
            </div>
          )}
          <div className="rounded-lg bg-muted/50 px-3 py-2 text-sm">
            <span className="text-xs text-muted-foreground">{t('settings.proxyPreview')}: </span>
            <code className="break-all font-mono text-[13px]">
              {draft.raw != null ? t('settings.proxyKeep') : buildProxyURL(p, true) || t('settings.proxyDirect')}
            </code>
          </div>
          <p className="text-xs text-muted-foreground">{t('settings.proxyHint')}</p>
        </div>
      )}
    </SectionCard>
  )
}
