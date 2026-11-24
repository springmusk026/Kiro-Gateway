import { ArrowLeft, Check, Loader2, Play } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useI18n } from '@/lib/i18n'
import { authApi } from '@/lib/queries.accounts'
import type { KiroProfile } from '@/lib/types'
import { cn } from '@/lib/utils'
import { errorInfo, type ErrorInfo } from '../lib'
import { ErrorCallout, FlowFooter, UrlBox, type FlowProps } from './shared'

type Stage =
  | { kind: 'idle' }
  | { kind: 'authorize'; step: 'kiro' | 'microsoft'; sessionId: string; url: string }
  | { kind: 'profile'; sessionId: string; selectionId: string; profiles: KiroProfile[] }

function Stepper({ current }: { current: 0 | 1 | 2 | 3 }) {
  const { t } = useI18n()
  const steps = [t('accounts.ms.stepKiro'), t('accounts.ms.stepMicrosoft'), t('accounts.ms.stepProfile')]
  return (
    <ol className="flex items-center gap-2 text-xs">
      {steps.map((s, i) => (
        <li key={s} className="flex items-center gap-2">
          <span
            className={cn(
              'grid size-5 place-items-center rounded-full border text-[10px] font-semibold tabular transition-colors',
              i < current && 'border-primary bg-primary text-primary-foreground',
              i === current && 'border-primary text-primary',
              i > current && 'text-muted-foreground',
            )}
          >
            {i < current ? <Check className="size-3" /> : i + 1}
          </span>
          <span className={cn(i === current ? 'font-medium' : 'text-muted-foreground')}>{s}</span>
          {i < steps.length - 1 && <span className="h-px w-4 bg-border sm:w-8" />}
        </li>
      ))}
    </ol>
  )
}

export function MicrosoftFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [stage, setStage] = useState<Stage>({ kind: 'idle' })
  const [callback, setCallback] = useState('')
  const [selectedArn, setSelectedArn] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)
  const gen = useRef(0)
  const live = useRef<{ sessionId?: string; selectionId?: string }>({})

  const cancelServer = (ids: { sessionId?: string; selectionId?: string }) => {
    if (ids.sessionId || ids.selectionId) authApi.msCancel(ids.sessionId, ids.selectionId).catch(() => {})
  }
  /** Drop local state; optionally tell the server to cancel. Bumps the generation so stale responses are ignored. */
  const reset = (notify: boolean) => {
    gen.current++
    if (notify) cancelServer(live.current)
    live.current = {}
    setStage({ kind: 'idle' })
    setCallback('')
    setSelectedArn('')
    setError(null)
    setBusy(false)
  }
  useEffect(
    () => () => {
      gen.current++
      cancelServer(live.current)
    },
    [],
  )

  const guard = async <T,>(fn: () => Promise<T>, onOk: (r: T) => void) => {
    const g = gen.current
    setBusy(true)
    setError(null)
    try {
      const r = await fn()
      if (g !== gen.current) {
        // The user cancelled while waiting: release whatever the server just created.
        const x = r as { sessionId?: string; selectionId?: string }
        cancelServer({ sessionId: x.sessionId, selectionId: x.selectionId })
        return
      }
      onOk(r)
    } catch (e) {
      if (g === gen.current) setError(errorInfo(e))
    } finally {
      if (g === gen.current) setBusy(false)
    }
  }

  const start = () =>
    guard(authApi.msStart, (r) => {
      if (!r.sessionId || !r.authorizeUrl) {
        setError({ message: t('accounts.ms.invalidResponse') })
        return
      }
      live.current = { sessionId: r.sessionId }
      setStage({ kind: 'authorize', step: 'kiro', sessionId: r.sessionId, url: r.authorizeUrl })
    })

  const finish = (account: { id: string; email: string }, warning?: string) => {
    gen.current++
    live.current = {}
    onSuccess([account], t('accounts.ms.success'), warning)
  }

  const complete = () => {
    if (stage.kind !== 'authorize') return
    if (!callback.trim()) {
      setError({ message: t('accounts.ms.callbackRequired') })
      return
    }
    const sessionId = stage.sessionId
    guard(
      () => authApi.msComplete(sessionId, callback.trim()),
      (r) => {
        setCallback('')
        if (r.stage === 'profile' && r.requiresProfileSelection && r.selectionId && r.profiles?.length) {
          live.current = { sessionId, selectionId: r.selectionId }
          setSelectedArn(r.profiles[0].arn)
          setStage({ kind: 'profile', sessionId, selectionId: r.selectionId, profiles: r.profiles })
        } else if (r.stage === 'complete' && r.account) {
          finish(r.account, r.warning)
        } else if (r.stage === 'microsoft' && r.authorizeUrl) {
          setStage({ kind: 'authorize', step: 'microsoft', sessionId, url: r.authorizeUrl })
        } else {
          setError({ message: t('accounts.ms.invalidResponse') })
        }
      },
    )
  }

  const select = () => {
    if (stage.kind !== 'profile') return
    if (!selectedArn) {
      setError({ message: t('accounts.ms.profileRequired') })
      return
    }
    const selectionId = stage.selectionId
    guard(
      () => authApi.msSelectProfile(selectionId, selectedArn),
      (r) => {
        if (r.account) finish(r.account, r.warning)
        else setError({ message: t('accounts.ms.invalidResponse') })
      },
    )
  }

  const back = () => {
    if (stage.kind === 'idle') onBack()
    else reset(true)
  }

  return (
    <div className="space-y-4">
      <Stepper current={stage.kind === 'idle' ? 0 : stage.kind === 'profile' ? 2 : stage.step === 'kiro' ? 0 : 1} />

      {stage.kind === 'idle' && <p className="text-sm text-muted-foreground">{t('accounts.ms.intro')}</p>}

      {stage.kind === 'authorize' && (
        <div key={stage.step + stage.url} className="space-y-4 animate-rise">
          <UrlBox url={stage.url} label={stage.step === 'kiro' ? t('accounts.ms.portalStep') : t('accounts.ms.providerStep')} />
          <p className="text-sm text-muted-foreground">{t('accounts.ms.callbackInstructions')}</p>
          <div className="space-y-1.5">
            <Label htmlFor="ms-cb">{t('accounts.ms.callbackUrl')}</Label>
            <Textarea
              id="ms-cb"
              value={callback}
              onChange={(e) => setCallback(e.target.value)}
              placeholder="http://localhost:3128/.../?..."
              rows={3}
              className="font-mono text-xs"
              spellCheck={false}
            />
          </div>
        </div>
      )}

      {stage.kind === 'profile' && (
        <div className="space-y-3 animate-rise">
          <div>
            <p className="text-sm font-medium">{t('accounts.ms.selectProfileTitle')}</p>
            <p className="text-xs text-muted-foreground">{t('accounts.ms.selectProfileDesc')}</p>
          </div>
          <div role="radiogroup" className="max-h-64 space-y-2 overflow-y-auto">
            {stage.profiles.map((p) => (
              <button
                key={p.arn}
                type="button"
                role="radio"
                aria-checked={selectedArn === p.arn}
                onClick={() => setSelectedArn(p.arn)}
                className={cn(
                  'flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:border-primary/40',
                  selectedArn === p.arn && 'border-primary bg-primary/5 ring-1 ring-primary/30',
                )}
              >
                <span
                  className={cn(
                    'mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border',
                    selectedArn === p.arn && 'border-primary',
                  )}
                >
                  {selectedArn === p.arn && <span className="size-2 rounded-full bg-primary" />}
                </span>
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{p.name || p.arn}</span>
                  <span className="block truncate font-mono text-[11px] text-muted-foreground">{p.arn}</span>
                  <span className="block text-xs text-muted-foreground">{t('accounts.ms.profileRegion', { region: p.region || '–' })}</span>
                </span>
              </button>
            ))}
          </div>
        </div>
      )}

      <ErrorCallout error={error} />

      <FlowFooter>
        <Button variant="ghost" onClick={back}>
          <ArrowLeft /> {stage.kind === 'idle' ? t('common.back') : t('accounts.ms.startOver')}
        </Button>
        {stage.kind === 'idle' && (
          <Button onClick={start} disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : <Play />} {busy ? t('accounts.ms.processing') : t('accounts.ms.start')}
          </Button>
        )}
        {stage.kind === 'authorize' && (
          <Button onClick={complete} disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : <Check />} {busy ? t('accounts.ms.processing') : t('accounts.ms.complete')}
          </Button>
        )}
        {stage.kind === 'profile' && (
          <Button onClick={select} disabled={busy || !selectedArn}>
            {busy ? <Loader2 className="animate-spin" /> : <Check />} {busy ? t('accounts.ms.processing') : t('accounts.ms.selectProfile')}
          </Button>
        )}
      </FlowFooter>
    </div>
  )
}
