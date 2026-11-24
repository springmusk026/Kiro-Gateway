import { CheckCircle2, FlaskConical, Loader2, Trash2, XCircle } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import { accountApi, useCachedAccountModels } from '@/lib/queries.accounts'
import type { Account } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AuthBadge, HealthDot } from './account-bits'
import { useAccountActions } from './actions'
import { errMsg } from './lib'

interface Run {
  id: number
  model: string
  at: Date
  ms?: number
  ok?: boolean
  reply?: string
  error?: string
}

const DEFAULT_MODEL = 'claude-sonnet-4'

export function TestDialog({ account, onClose }: { account: Account | undefined; onClose: () => void }) {
  const { t } = useI18n()
  return (
    <Dialog open={!!account} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FlaskConical className="size-5 text-primary" /> {t('accounts.test.title')}
          </DialogTitle>
          <DialogDescription>{t('accounts.test.description')}</DialogDescription>
        </DialogHeader>
        {account && <TestBody key={account.id} account={account} />}
      </DialogContent>
    </Dialog>
  )
}

function TestBody({ account: a }: { account: Account }) {
  const { t, lang } = useI18n()
  const act = useAccountActions()
  const cached = useCachedAccountModels(a.id, true)
  const catalog = useModels()
  const [model, setModel] = useState('')
  const [runs, setRuns] = useState<Run[]>([])
  const [running, setRunning] = useState(false)
  const abort = useRef<AbortController | null>(null)
  const seq = useRef(0)
  useEffect(() => () => abort.current?.abort(), [])

  const { options, source } = useMemo(() => {
    if (cached.data?.length) return { options: cached.data, source: 'account' as const }
    const cat = (catalog.data?.models ?? []).filter((m) => m.enabled).map((m) => m.id)
    if (cat.length) return { options: cat, source: 'catalog' as const }
    return { options: [] as string[], source: 'none' as const }
  }, [cached.data, catalog.data])
  const loadingModels = cached.isLoading || (source !== 'account' && catalog.isLoading)

  useEffect(() => {
    if (model || loadingModels) return
    const pref = options.find((m) => m === DEFAULT_MODEL) ?? options.find((m) => m.includes('sonnet')) ?? options[0]
    setModel(pref ?? DEFAULT_MODEL)
  }, [options, loadingModels, model])

  const run = async () => {
    const m = model.trim()
    if (!m || running) return
    const id = ++seq.current
    const ctrl = new AbortController()
    abort.current = ctrl
    setRunning(true)
    setRuns((r) => [{ id, model: m, at: new Date() }, ...r].slice(0, 50))
    const t0 = performance.now()
    try {
      const res = await accountApi.test(a.id, m, ctrl.signal)
      const ms = performance.now() - t0
      setRuns((r) => r.map((x) => (x.id === id ? { ...x, ms, ok: true, reply: res.reply } : x)))
    } catch (e) {
      if (ctrl.signal.aborted) return
      const ms = performance.now() - t0
      setRuns((r) => r.map((x) => (x.id === id ? { ...x, ms, ok: false, error: errMsg(e) } : x)))
    } finally {
      if (!ctrl.signal.aborted) setRunning(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm">
        <HealthDot account={a} />
        <span className="truncate font-medium">{act.name(a)}</span>
        <AuthBadge account={a} />
        <span className="ml-auto truncate font-mono text-xs text-muted-foreground">
          {a.proxyURL ? t('accounts.test.viaProxy', { proxy: a.proxyURL }) : t('accounts.test.globalProxy')}
        </span>
      </div>

      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          run()
        }}
      >
        <div className="min-w-0 flex-1 space-y-1.5">
          <Label htmlFor="test-model">{t('accounts.test.model')}</Label>
          {options.length ? (
            <Select value={model} onValueChange={setModel} disabled={loadingModels}>
              <SelectTrigger id="test-model" className="w-full font-mono text-xs">
                <SelectValue placeholder={t('common.loading')} />
              </SelectTrigger>
              <SelectContent className="max-h-72">
                {options.map((m) => (
                  <SelectItem key={m} value={m} className="font-mono text-xs">
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              id="test-model"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="font-mono text-xs"
              placeholder={DEFAULT_MODEL}
              disabled={loadingModels}
            />
          )}
        </div>
        <Button type="submit" disabled={running || loadingModels || !model.trim()}>
          {running ? <Loader2 className="animate-spin" /> : <FlaskConical />} {t('accounts.test.run')}
        </Button>
      </form>
      <p className="-mt-2 text-xs text-muted-foreground">
        {loadingModels
          ? t('accounts.test.modelsLoading')
          : source === 'account'
            ? t('accounts.test.modelsFromAccount', { n: options.length })
            : source === 'catalog'
              ? t('accounts.test.modelsFromCatalog')
              : t('accounts.test.modelsManual')}
      </p>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <p className="text-xs font-medium text-muted-foreground">{t('accounts.test.results')}</p>
          {runs.length > 0 && (
            <Button size="xs" variant="ghost" onClick={() => setRuns([])} disabled={running}>
              <Trash2 /> {t('accounts.test.clear')}
            </Button>
          )}
        </div>
        <div className="max-h-72 space-y-2 overflow-y-auto">
          {runs.length === 0 && (
            <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">{t('accounts.test.empty')}</p>
          )}
          {runs.map((r) => (
            <div
              key={r.id}
              className={cn(
                'rounded-lg border p-3 text-sm animate-rise',
                r.ok === true && 'border-success/30 bg-success/5',
                r.ok === false && 'border-destructive/30 bg-destructive/5',
              )}
            >
              <div className="flex items-center gap-2 text-xs">
                {r.ok === undefined ? (
                  <Loader2 className="size-4 animate-spin text-muted-foreground" />
                ) : r.ok ? (
                  <CheckCircle2 className="size-4 text-success" />
                ) : (
                  <XCircle className="size-4 text-destructive" />
                )}
                <span className="font-mono">{r.model}</span>
                <span className="ml-auto text-muted-foreground tabular">
                  {r.ms !== undefined && `${(r.ms / 1000).toFixed(2)}s · `}
                  {r.at.toLocaleTimeString(lang)}
                </span>
              </div>
              {r.ok === undefined && <p className="mt-1.5 text-xs text-muted-foreground">{t('accounts.test.waiting')}</p>}
              {r.ok && (
                <p className="mt-1.5 whitespace-pre-wrap break-words">
                  <span className="text-muted-foreground">{t('accounts.test.reply')} </span>
                  {r.reply || <em className="text-muted-foreground">{t('accounts.test.emptyReply')}</em>}
                </p>
              )}
              {r.ok === false && <p className="mt-1.5 whitespace-pre-wrap break-words font-mono text-xs text-destructive">{r.error}</p>}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
