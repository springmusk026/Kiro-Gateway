import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ArrowUp, Bot, Brain, Copy, RotateCcw, Settings2, Sparkles, Square, SquarePen, Terminal, User } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { Markdown } from '@/components/playground/markdown'
import { takePrefill } from '@/components/playground/prefill'
import { buildRequest, runChat, type ChatMessage, type Protocol } from '@/components/playground/stream'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/_app/playground')({
  validateSearch: (s: Record<string, unknown>): { model?: string; prefill?: number } => ({
    ...(typeof s.model === 'string' ? { model: s.model } : {}),
    // Set by "Open in Playground" in Request logs; the content itself is in sessionStorage.
    ...(s.prefill ? { prefill: 1 } : {}),
  }),
  component: PlaygroundPage,
})

interface Turn extends ChatMessage {
  thinking?: string
  error?: string
  pending?: boolean
  metrics?: { ttft?: number; total: number; input?: number; output?: number }
}

const STORE = 'kiro_playground'

function loadPrefs() {
  try {
    return JSON.parse(localStorage.getItem(STORE) || '{}') as Partial<Prefs>
  } catch {
    return {}
  }
}
interface Prefs {
  model: string
  thinking: boolean
  effort: string
  protocol: Protocol
  system: string
  temperature: number
  maxTokens: number
  stream: boolean
}

function PlaygroundPage() {
  const { t } = useI18n()
  const search = Route.useSearch()
  const { data: catalog } = useModels()
  const models = useMemo(() => (catalog?.models ?? []).filter((m) => m.enabled).map((m) => m.id), [catalog])

  const [prefs, setPrefs] = useState<Prefs>(() => ({
    model: '',
    thinking: false,
    effort: 'auto',
    protocol: 'anthropic',
    system: '',
    temperature: 0.7,
    maxTokens: 4096,
    stream: true,
    ...loadPrefs(),
    ...(search.model ? { model: search.model } : {}),
  }))
  const set = <K extends keyof Prefs>(k: K, v: Prefs[K]) => setPrefs((p) => ({ ...p, [k]: v }))
  useEffect(() => {
    try {
      localStorage.setItem(STORE, JSON.stringify(prefs))
    } catch {
      /* storage unavailable */
    }
  }, [prefs])
  useEffect(() => {
    if (models.length && !models.includes(prefs.model)) set('model', models.find((m) => m.includes('sonnet')) ?? models[0])
  }, [models]) // eslint-disable-line react-hooks/exhaustive-deps

  const [turns, setTurns] = useState<Turn[]>([])
  const [input, setInput] = useState('')
  const navigate = useNavigate()
  // "Open in Playground": load the hand-over once, then drop the search flag.
  const prefillRead = useRef(false)
  useEffect(() => {
    if (!search.prefill || prefillRead.current) return
    prefillRead.current = true
    const p = takePrefill()
    navigate({ to: '/playground', search: {}, replace: true })
    if (!p) {
      toast.error(t('playground.prefillMissing'))
      return
    }
    const msgs = [...p.messages]
    // A trailing user message goes into the composer, ready to send or edit.
    const pending = msgs.length && msgs[msgs.length - 1].role === 'user' ? msgs.pop()!.content : ''
    setPrefs((prev) => ({ ...prev, model: p.model || prev.model, protocol: p.protocol, system: p.system }))
    setTurns(msgs)
    setInput(pending)
    toast.success(t('playground.prefilled', { n: p.messages.length }), {
      description: p.stripped ? t('playground.prefillStripped', { n: p.stripped }) : undefined,
    })
  }, [search.prefill]) // eslint-disable-line react-hooks/exhaustive-deps
  const [settingsOpen, setSettingsOpen] = useState(false)
  const ctrl = useRef<AbortController | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const busy = turns.some((x) => x.pending)
  const effortLevels = catalog?.models.find((m) => m.id === prefs.model)?.effortLevels

  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  }, [turns])

  const send = async (text: string, history: Turn[] = turns) => {
    if (!text.trim() || busy || !prefs.model) return
    const base: Turn[] = [...history, { role: 'user', content: text.trim() }]
    const reply: Turn = { role: 'assistant', content: '', thinking: '', pending: true }
    setTurns([...base, reply])
    setInput('')
    const started = performance.now()
    let first = 0
    const usage: { input?: number; output?: number } = {}
    const update = (patch: Partial<Turn>) =>
      setTurns((all) => all.map((x, i) => (i === all.length - 1 ? { ...x, ...patch } : x)))
    let content = ''
    let thinking = ''
    ctrl.current = new AbortController()
    try {
      await runChat(
        {
          protocol: prefs.protocol,
          model: prefs.model,
          thinking: prefs.thinking,
          effort: prefs.effort,
          system: prefs.system,
          temperature: prefs.temperature,
          maxTokens: prefs.maxTokens,
          stream: prefs.stream,
          messages: base.filter((x) => !x.error && x.content).map(({ role, content }) => ({ role, content })),
        },
        {
          onText: (d) => {
            if (!first) first = performance.now()
            content += d
            update({ content })
          },
          onThinking: (d) => {
            if (!first) first = performance.now()
            thinking += d
            update({ thinking })
          },
          onUsage: (u) => Object.assign(usage, Object.fromEntries(Object.entries(u).filter(([, v]) => v != null))),
        },
        ctrl.current.signal,
      )
      update({ pending: false, metrics: { ttft: first ? first - started : undefined, total: performance.now() - started, ...usage } })
    } catch (e) {
      const aborted = (e as Error).name === 'AbortError'
      update({
        pending: false,
        error: aborted ? (content ? undefined : t('playground.stopped')) : (e as Error).message,
        metrics: { total: performance.now() - started },
      })
    } finally {
      ctrl.current = null
    }
  }

  const regenerate = () => {
    const lastUser = [...turns].reverse().find((x) => x.role === 'user')
    if (!lastUser) return
    const idx = turns.lastIndexOf(lastUser)
    send(lastUser.content, turns.slice(0, idx))
  }

  const copyCurl = async () => {
    const msgs = turns.filter((x) => x.content && !x.error).map(({ role, content }) => ({ role, content }))
    const req = buildRequest({
      protocol: prefs.protocol,
      model: prefs.model,
      thinking: prefs.thinking,
      effort: prefs.effort,
      system: prefs.system,
      temperature: prefs.temperature,
      maxTokens: prefs.maxTokens,
      stream: prefs.stream,
      messages: msgs.length ? msgs : [{ role: 'user', content: 'Hello' }],
    })
    const auth =
      prefs.protocol === 'anthropic'
        ? `  -H "x-api-key: $KIRO_API_KEY" \\\n  -H "anthropic-version: 2023-06-01" \\\n`
        : `  -H "Authorization: Bearer $KIRO_API_KEY" \\\n`
    const cmd = `curl ${window.location.origin}${req.publicPath} \\\n${auth}  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify(req.body).replace(/'/g, "'\\''")}'`
    await copyToClipboard(cmd)
    toast.success(t('common.copied'))
  }

  const settings = (
    <SettingsPanel prefs={prefs} set={set} models={models} effortLevels={effortLevels} onCurl={copyCurl} />
  )

  return (
    <div className="grid h-[calc(100svh-7.5rem)] gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
      <Card className="relative flex min-h-0 flex-col gap-0 overflow-hidden py-0">
        <div className="flex items-center gap-2 border-b px-4 py-2.5">
          <Sparkles className="size-4 text-primary" />
          <span className="truncate font-mono text-sm">{prefs.model || '—'}</span>
          {prefs.thinking && (
            <Badge variant="secondary" className="gap-1 font-normal">
              <Brain className="size-3" />
              {prefs.effort === 'auto' ? t('playground.thinking') : prefs.effort}
            </Badge>
          )}
          <Badge variant="secondary" className="font-normal">{prefs.protocol === 'anthropic' ? 'Anthropic' : 'OpenAI'}</Badge>
          <div className="ml-auto flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" className="size-8" onClick={copyCurl} aria-label={t('playground.copyCurl')}>
                  <Terminal className="size-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('playground.copyCurl')}</TooltipContent>
            </Tooltip>
            <Button variant="ghost" size="sm" onClick={() => setTurns([])} disabled={busy || !turns.length}>
              <SquarePen className="size-4" />
              <span className="hidden sm:inline">{t('playground.clear')}</span>
            </Button>
            <Button variant="ghost" size="icon" className="size-8 lg:hidden" onClick={() => setSettingsOpen(true)} aria-label={t('playground.settings')}>
              <Settings2 className="size-4" />
            </Button>
          </div>
        </div>

        <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-8">
          {!turns.length ? (
            <div className="mx-auto flex h-full max-w-lg flex-col items-center justify-center text-center animate-rise">
              <div className="grid size-14 place-items-center rounded-2xl bg-gradient-to-br from-chart-1 to-chart-2 text-white shadow-lg shadow-primary/20">
                <Sparkles className="size-6" />
              </div>
              <h2 className="mt-5 text-xl font-semibold tracking-tight">{t('playground.emptyTitle')}</h2>
              <p className="mt-2 text-sm text-muted-foreground">{models.length ? t('playground.emptyText') : t('playground.noModels')}</p>
              <div className="mt-6 grid w-full gap-2 stagger">
                {(['playground.suggestion1', 'playground.suggestion2', 'playground.suggestion3'] as const).map((k) => (
                  <button
                    key={k}
                    disabled={!models.length}
                    onClick={() => send(t(k))}
                    className="rounded-xl border bg-card px-4 py-3 text-left text-sm transition-all hover:-translate-y-px hover:border-primary/40 hover:shadow-sm disabled:opacity-50"
                  >
                    {t(k)}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            <div className="mx-auto max-w-3xl space-y-6">
              {turns.map((turn, i) => (
                <TurnView key={i} turn={turn} last={i === turns.length - 1} onRetry={regenerate} />
              ))}
            </div>
          )}
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            send(input)
          }}
          className="border-t bg-card/60 p-3 sm:p-4"
        >
          <div className="mx-auto flex max-w-3xl items-end gap-2 rounded-2xl border bg-background p-2 shadow-xs transition-colors focus-within:border-primary/50">
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault()
                  send(input)
                }
              }}
              rows={1}
              placeholder={t('playground.placeholder', { model: prefs.model || '…' })}
              className="max-h-48 min-h-10 flex-1 resize-none border-0 bg-transparent shadow-none focus-visible:ring-0 dark:bg-transparent"
            />
            {busy ? (
              <Button type="button" size="icon" variant="secondary" className="size-9 rounded-xl" onClick={() => ctrl.current?.abort()} aria-label={t('playground.stop')}>
                <Square className="size-4 fill-current" />
              </Button>
            ) : (
              <Button type="submit" size="icon" className="size-9 rounded-xl" disabled={!input.trim() || !prefs.model} aria-label={t('playground.send')}>
                <ArrowUp className="size-4" />
              </Button>
            )}
          </div>
          <p className="mx-auto mt-1.5 max-w-3xl px-2 text-[11px] text-muted-foreground">{t('playground.sendHint')}</p>
        </form>
      </Card>

      <Card className="hidden min-h-0 overflow-y-auto p-5 lg:block">{settings}</Card>
      <Sheet open={settingsOpen} onOpenChange={setSettingsOpen}>
        <SheetContent side="right" className="w-80 overflow-y-auto">
          <SheetHeader>
            <SheetTitle>{t('playground.settings')}</SheetTitle>
          </SheetHeader>
          <div className="px-4 pb-6">{settings}</div>
        </SheetContent>
      </Sheet>
    </div>
  )
}

function SettingsPanel({
  prefs,
  set,
  models,
  effortLevels,
  onCurl,
}: {
  prefs: Prefs
  set: <K extends keyof Prefs>(k: K, v: Prefs[K]) => void
  models: string[]
  effortLevels?: string[]
  onCurl: () => void
}) {
  const { t } = useI18n()
  const levels = ['auto', ...(effortLevels?.length ? effortLevels : ['low', 'medium', 'high'])]
  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <Label>{t('playground.model')}</Label>
        <Select value={prefs.model} onValueChange={(v) => set('model', v)}>
          <SelectTrigger className="w-full font-mono text-[13px]">
            <SelectValue placeholder="—" />
          </SelectTrigger>
          <SelectContent>
            {models.map((m) => (
              <SelectItem key={m} value={m} className="font-mono text-[13px]">
                {m}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-3 rounded-xl border bg-muted/30 p-3">
        <div className="flex items-start justify-between gap-3">
          <div>
            <Label className="flex items-center gap-1.5">
              <Brain className="size-4 text-primary" /> {t('playground.thinking')}
            </Label>
            <p className="mt-1 text-xs text-muted-foreground">{t('playground.thinkingHint')}</p>
          </div>
          <Switch checked={prefs.thinking} onCheckedChange={(v) => set('thinking', v)} />
        </div>
        {prefs.thinking && (
          <div className="space-y-1.5 animate-rise">
            <Label className="text-xs text-muted-foreground">{t('playground.effort')}</Label>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              className="w-full"
              value={levels.includes(prefs.effort) ? prefs.effort : 'auto'}
              onValueChange={(v) => v && set('effort', v)}
            >
              {levels.map((l) => (
                <ToggleGroupItem key={l} value={l} className="flex-1 px-1 text-xs capitalize">
                  {l === 'auto' ? t('playground.effortAuto') : l}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        )}
      </div>
      <div className="space-y-2">
        <Label>{t('playground.format')}</Label>
        <Select value={prefs.protocol} onValueChange={(v) => set('protocol', v as Protocol)}>
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="anthropic">Anthropic · /v1/messages</SelectItem>
            <SelectItem value="openai">OpenAI · /v1/chat/completions</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label htmlFor="pg-system">{t('playground.system')}</Label>
        <Textarea id="pg-system" rows={4} value={prefs.system} onChange={(e) => set('system', e.target.value)} placeholder={t('playground.systemPlaceholder')} />
      </div>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <Label>{t('playground.temperature')}</Label>
          <span className="font-mono text-xs tabular text-muted-foreground">{prefs.temperature.toFixed(2)}</span>
        </div>
        <Slider min={0} max={1} step={0.05} value={[prefs.temperature]} onValueChange={([v]) => set('temperature', v)} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="pg-max">{t('playground.maxTokens')}</Label>
        <Input id="pg-max" type="number" min={1} max={64000} value={prefs.maxTokens} onChange={(e) => set('maxTokens', Math.max(1, Number(e.target.value) || 1))} />
      </div>
      <div className="flex items-center justify-between">
        <Label htmlFor="pg-stream">{t('playground.stream')}</Label>
        <Switch id="pg-stream" checked={prefs.stream} onCheckedChange={(v) => set('stream', v)} />
      </div>
      <Button variant="outline" className="w-full" onClick={onCurl}>
        <Terminal className="size-4" />
        {t('playground.copyCurl')}
      </Button>
    </div>
  )
}

function TurnView({ turn, last, onRetry }: { turn: Turn; last: boolean; onRetry: () => void }) {
  const { t } = useI18n()
  const isUser = turn.role === 'user'
  const m = turn.metrics
  return (
    <div className={cn('group flex gap-3 animate-rise', isUser && 'flex-row-reverse')}>
      <div
        className={cn(
          'grid size-8 shrink-0 place-items-center rounded-lg',
          isUser ? 'bg-muted text-muted-foreground' : 'bg-gradient-to-br from-chart-1 to-chart-2 text-white',
        )}
      >
        {isUser ? <User className="size-4" /> : <Bot className="size-4" />}
      </div>
      <div className={cn('min-w-0 max-w-[85%]', isUser && 'flex flex-col items-end')}>
        {turn.thinking ? (
          <Collapsible defaultOpen={turn.pending} className="mb-2">
            <CollapsibleTrigger className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
              <Brain className={cn('size-3.5', turn.pending && !turn.content && 'animate-pulse text-primary')} />
              {turn.pending && !turn.content ? t('playground.reasoningLive') : t('playground.reasoning')}
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-2 border-l-2 pl-3 text-sm text-muted-foreground">
              <p className="whitespace-pre-wrap">{turn.thinking}</p>
            </CollapsibleContent>
          </Collapsible>
        ) : null}
        <div
          className={cn(
            'rounded-2xl',
            isUser ? 'bg-primary px-4 py-2.5 text-primary-foreground' : 'px-1 py-1',
            turn.error && 'rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-2.5 text-destructive',
          )}
        >
          {turn.error ? (
            <p className="text-sm">{turn.error}</p>
          ) : isUser ? (
            <p className="whitespace-pre-wrap text-[14.5px]">{turn.content}</p>
          ) : turn.content ? (
            <Markdown text={turn.content} />
          ) : turn.pending ? (
            <span className="inline-flex gap-1 py-2">
              {[0, 1, 2].map((i) => (
                <span key={i} className="size-1.5 animate-bounce rounded-full bg-muted-foreground/60" style={{ animationDelay: `${i * 120}ms` }} />
              ))}
            </span>
          ) : null}
        </div>
        {!isUser && !turn.pending && (
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
            {m?.ttft != null && <span>{t('playground.ttft', { s: (m.ttft / 1000).toFixed(2) + 's' })}</span>}
            {m && <span>{t('playground.total', { s: (m.total / 1000).toFixed(2) + 's' })}</span>}
            {m?.output && m.total > 0 && <span>{t('playground.tps', { n: Math.round(m.output / (m.total / 1000)) })}</span>}
            {(m?.input || m?.output) && <span>{t('playground.tokens', { in: m?.input ?? 0, out: m?.output ?? 0 })}</span>}
            <span className="flex opacity-0 transition-opacity group-hover:opacity-100">
              {turn.content && (
                <Button variant="ghost" size="icon" className="size-6" aria-label={t('playground.copy')} onClick={() => copyToClipboard(turn.content).then(() => toast.success(t('common.copied')))}>
                  <Copy className="size-3" />
                </Button>
              )}
              {last && (
                <Button variant="ghost" size="icon" className="size-6" aria-label={t('playground.retry')} onClick={onRetry}>
                  <RotateCcw className="size-3" />
                </Button>
              )}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}
