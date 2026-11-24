import {
  Building2,
  ChevronRight,
  Cookie,
  FileJson,
  FolderOpen,
  KeyRound,
  KeySquare,
  ShieldCheck,
  UserRound,
  type LucideIcon,
} from 'lucide-react'
import { useState, type ComponentType } from 'react'
import { toast } from 'sonner'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { useI18n } from '@/lib/i18n'
import type { NewAccountRef } from '@/lib/types'
import type { MessageKey } from '@/locales/en'
import { BuilderIdFlow } from './builder-id'
import { CredentialsFlow } from './credentials'
import { IamFlow } from './iam'
import { LocalCacheFlow } from './local-cache'
import { MicrosoftFlow } from './microsoft'
import { ApiKeyFlow, SocialTokenFlow } from './simple'
import { SsoTokenFlow } from './sso-token'
import type { FlowProps } from './shared'

type Method = 'builderid' | 'iam' | 'microsoft' | 'sso' | 'local' | 'credentials' | 'social' | 'apikey'

const METHODS: { id: Method; icon: LucideIcon; title: MessageKey; desc: MessageKey; tag?: MessageKey; Flow: ComponentType<FlowProps> }[] = [
  { id: 'builderid', icon: UserRound, title: 'accounts.method.builderid', desc: 'accounts.method.builderidDesc', tag: 'accounts.method.recommended', Flow: BuilderIdFlow },
  { id: 'iam', icon: Building2, title: 'accounts.method.iam', desc: 'accounts.method.iamDesc', Flow: IamFlow },
  { id: 'microsoft', icon: ShieldCheck, title: 'accounts.method.microsoft', desc: 'accounts.method.microsoftDesc', Flow: MicrosoftFlow },
  { id: 'sso', icon: KeySquare, title: 'accounts.method.sso', desc: 'accounts.method.ssoDesc', tag: 'accounts.method.bulk', Flow: SsoTokenFlow },
  { id: 'local', icon: FolderOpen, title: 'accounts.method.local', desc: 'accounts.method.localDesc', Flow: LocalCacheFlow },
  { id: 'credentials', icon: FileJson, title: 'accounts.method.credentials', desc: 'accounts.method.credentialsDesc', tag: 'accounts.method.bulk', Flow: CredentialsFlow },
  { id: 'social', icon: Cookie, title: 'accounts.method.social', desc: 'accounts.method.socialDesc', Flow: SocialTokenFlow },
  { id: 'apikey', icon: KeyRound, title: 'accounts.method.apikey', desc: 'accounts.method.apikeyDesc', Flow: ApiKeyFlow },
]

export function AddAccountDialog({
  open,
  onOpenChange,
  onAdded,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onAdded: (refs: NewAccountRef[]) => void
}) {
  const { t } = useI18n()
  const [method, setMethod] = useState<Method | null>(null)
  const current = METHODS.find((m) => m.id === method)

  const close = () => {
    onOpenChange(false)
    // Let the close animation finish before resetting to the picker.
    setTimeout(() => setMethod(null), 200)
  }

  const props: FlowProps = {
    onBack: () => setMethod(null),
    onAdded,
    onClose: close,
    onSuccess: (refs, title, warning) => {
      onAdded(refs)
      const who = refs.map((r) => r.email || r.id).filter(Boolean)
      toast.success(title, { description: who.length === 1 ? who[0] : who.length ? t('accounts.add.nAccounts', { n: who.length }) : undefined })
      if (warning) toast.warning(warning, { duration: 8000 })
      close()
    },
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) close()
        else onOpenChange(true)
      }}
    >
      <DialogContent className="max-h-[92svh] overflow-y-auto sm:max-w-2xl" onInteractOutside={(e) => method && e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {current && (
              <span className="grid size-7 place-items-center rounded-lg bg-accent text-accent-foreground">
                <current.icon className="size-4" />
              </span>
            )}
            {current ? t(current.title) : t('accounts.add.title')}
          </DialogTitle>
          <DialogDescription>{current ? t(current.desc) : t('accounts.add.description')}</DialogDescription>
        </DialogHeader>
        {current ? (
          <div key={current.id} className="animate-rise">
            <current.Flow {...props} />
          </div>
        ) : (
          <div className="grid gap-2.5 sm:grid-cols-2 stagger">
            {METHODS.map((m) => (
              <button
                key={m.id}
                type="button"
                onClick={() => setMethod(m.id)}
                className="group flex items-start gap-3 rounded-xl border bg-card p-3.5 text-left transition-all hover:-translate-y-px hover:border-primary/40 hover:shadow-sm focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground transition-transform group-hover:scale-105">
                  <m.icon className="size-[18px]" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5 text-sm font-medium">
                    {t(m.title)}
                    {m.tag && (
                      <span className="rounded bg-primary/10 px-1 py-px text-[10px] font-medium uppercase tracking-wide text-primary">{t(m.tag)}</span>
                    )}
                  </span>
                  <span className="mt-0.5 block text-xs leading-snug text-muted-foreground">{t(m.desc)}</span>
                </span>
                <ChevronRight className="mt-2 size-4 shrink-0 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5 group-hover:text-primary" />
              </button>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
