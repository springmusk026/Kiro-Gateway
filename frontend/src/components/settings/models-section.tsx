import { ArrowRight, Brain, Plus, Shuffle, Trash2 } from 'lucide-react'
import { useId, useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import { useModelMappings, useSaveModelMappings, useSaveThinking, useThinking } from '@/lib/queries.settings'
import type { ModelMapping, ThinkingConfig, ThinkingFormat, ThinkingMode } from '@/lib/types'
import { cn } from '@/lib/utils'
import { SectionCard, runSave, useDraft } from './section-card'

const FORMATS: ThinkingFormat[] = ['reasoning_content', 'thinking', 'think']
const DEFAULT_SUFFIX = '-thinking'
const MODES: ThinkingMode[] = ['auto', 'always', 'never']
const DEFAULT_BUDGET = 200000

function FormatSelect({ id, value, onChange }: { id: string; value: string; onChange: (v: ThinkingFormat) => void }) {
  const { t } = useI18n()
  const v = (value || 'reasoning_content') as ThinkingFormat
  return (
    <div className="space-y-1">
    <Select value={v} onValueChange={(x) => onChange(x as ThinkingFormat)}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {FORMATS.map((f) => (
          <SelectItem key={f} value={f}>
            <span className="font-mono text-[13px]">{f}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
    <p className="text-xs text-muted-foreground">{t(`settings.fmt.${v}` as 'settings.fmt.think')}</p>
    </div>
  )
}

export function ThinkingCard() {
  const { t } = useI18n()
  const q = useThinking()
  const models = useModels()
  const save = useSaveThinking()
  const { draft, setDraft, dirty, reset, commit } = useDraft<ThinkingConfig>(q.data)
  const example = models.data?.models.find((m) => m.enabled && m.id.startsWith('claude'))?.id ?? 'claude-sonnet-4.5'
  const suffix = draft?.suffix || DEFAULT_SUFFIX
  const suffixInvalid = !!draft?.suffix && /\s/.test(draft.suffix)
  const budget = draft?.budget ?? 0
  const budgetInvalid = budget !== 0 && (budget < 1024 || budget > DEFAULT_BUDGET)
  const mode: ThinkingMode = draft?.mode || 'auto'

  return (
    <SectionCard
      icon={Brain}
      title={t('settings.thinking')}
      description={t('settings.thinkingDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={suffixInvalid || budgetInvalid}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync(draft), t('settings.saved')))) commit()
      }}
    >
      {draft && (
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="th-mode">{t('settings.thinkingMode')}</Label>
              <Select value={mode} onValueChange={(v) => setDraft((d) => ({ ...d, mode: v as ThinkingMode }))}>
                <SelectTrigger id="th-mode" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MODES.map((m) => (
                    <SelectItem key={m} value={m}>
                      {t(`settings.mode.${m}` as const)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t(`settings.mode.${mode}Hint` as const)}</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="th-budget">{t('settings.thinkingBudget')}</Label>
              <Input
                id="th-budget"
                type="number"
                inputMode="numeric"
                min={1024}
                max={DEFAULT_BUDGET}
                step={1024}
                className="tabular"
                placeholder={DEFAULT_BUDGET.toLocaleString()}
                value={budget && budget !== DEFAULT_BUDGET ? budget : ''}
                aria-invalid={budgetInvalid}
                onChange={(e) => setDraft((d) => ({ ...d, budget: e.target.value ? Math.round(Number(e.target.value)) : 0 }))}
              />
              <p className={cn('text-xs', budgetInvalid ? 'text-destructive' : 'text-muted-foreground')}>
                {t(budgetInvalid ? 'settings.thinkingBudgetInvalid' : 'settings.thinkingBudgetHint')}
              </p>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="th-suffix">{t('settings.thinkingSuffix')}</Label>
              <Input
                id="th-suffix"
                className="font-mono"
                placeholder={DEFAULT_SUFFIX}
                value={draft.suffix}
                aria-invalid={suffixInvalid}
                onChange={(e) => setDraft((d) => ({ ...d, suffix: e.target.value }))}
              />
              {suffixInvalid && <p className="text-xs text-destructive">{t('settings.suffixWhitespace')}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="th-openai">{t('settings.openaiFormat')}</Label>
              <FormatSelect id="th-openai" value={draft.openaiFormat} onChange={(v) => setDraft((d) => ({ ...d, openaiFormat: v }))} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="th-claude">{t('settings.claudeFormat')}</Label>
              <FormatSelect id="th-claude" value={draft.claudeFormat} onChange={(v) => setDraft((d) => ({ ...d, claudeFormat: v }))} />
            </div>
          </div>
          <div className="rounded-lg border bg-muted/40 p-3 text-sm">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t('settings.example')}</p>
            <p className="mt-1.5 flex flex-wrap items-center gap-2">
              <code className="rounded bg-background px-1.5 py-0.5 font-mono text-[13px]">
                {example}
                <span className="text-primary">{suffix}</span>
              </code>
              <ArrowRight className="size-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">{t('settings.exampleResult', { model: example })}</span>
            </p>
            <p className="mt-2 text-xs text-muted-foreground">{t('settings.effortNote')}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t('settings.exampleFormats', { openai: draft.openaiFormat || 'reasoning_content', claude: draft.claudeFormat || 'thinking' })}
            </p>
          </div>
        </div>
      )}
    </SectionCard>
  )
}

// ---------- Model mappings ----------

type Row = ModelMapping & { key: string }
const MAX_MAPPINGS = 200
const MAX_ID_BYTES = 256
// eslint-disable-next-line no-control-regex
const BAD_ID = /[\s\u0000-\u001f\u007f]/

function validate(rows: Row[]): { rows: Record<string, { source?: string; target?: string }>; global?: string } {
  const out: Record<string, { source?: string; target?: string }> = {}
  const set = (k: string, f: 'source' | 'target', msg: string) => {
    out[k] = { ...out[k], [f]: out[k]?.[f] ?? msg }
  }
  const bytes = (s: string) => new TextEncoder().encode(s).length
  const sources = new Map<string, number>()
  rows.forEach((r) => {
    const s = r.source.trim()
    const tg = r.target.trim()
    if (!s) set(r.key, 'source', 'settings.mapRequired')
    else if (BAD_ID.test(s) || bytes(s) > MAX_ID_BYTES) set(r.key, 'source', 'settings.mapInvalidId')
    if (!tg) set(r.key, 'target', 'settings.mapRequired')
    else if (BAD_ID.test(tg) || bytes(tg) > MAX_ID_BYTES) set(r.key, 'target', 'settings.mapInvalidId')
    if (s && tg && s.toLowerCase() === tg.toLowerCase()) set(r.key, 'target', 'settings.mapSame')
    if (s) {
      const k = s.toLowerCase()
      if (sources.has(k)) set(r.key, 'source', 'settings.mapDuplicate')
      else sources.set(k, 1)
    }
  })
  rows.forEach((r) => {
    const tg = r.target.trim().toLowerCase()
    if (tg && sources.has(tg) && tg !== r.source.trim().toLowerCase()) set(r.key, 'target', 'settings.mapChain')
  })
  return { rows: out, global: rows.length > MAX_MAPPINGS ? 'settings.mapTooMany' : undefined }
}

let seq = 0
const newKey = () => `m${Date.now().toString(36)}${seq++}`

export function MappingsCard() {
  const { t } = useI18n()
  const q = useModelMappings()
  const models = useModels()
  const save = useSaveModelMappings()
  const listId = useId()
  const server = useMemo(() => q.data?.map((m, i) => ({ ...m, key: `s${i}` })), [q.data])
  const { draft, setDraft, dirty, reset, commit } = useDraft<Row[]>(server)
  const errors = useMemo(() => (draft ? validate(draft) : { rows: {} }), [draft])
  const hasErrors = !!errors.global || Object.keys(errors.rows).length > 0
  const ids = models.data?.models.map((m) => m.id) ?? []
  const update = (key: string, patch: Partial<ModelMapping>) =>
    setDraft((rows) => rows.map((r) => (r.key === key ? { ...r, ...patch } : r)))

  return (
    <SectionCard
      icon={Shuffle}
      title={t('settings.mappings')}
      description={t('settings.mappingsDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={hasErrors}
      onReset={reset}
      error={dirty && hasErrors ? t((errors.global ?? 'settings.mapFix') as 'settings.mapFix') : null}
      onSave={async () => {
        if (!draft) return
        const payload = draft.map(({ source, target }) => ({ source: source.trim(), target: target.trim() }))
        if (await runSave(() => save.mutateAsync(payload), t('settings.saved'))) commit()
      }}
    >
      {draft && (
        <div className="space-y-3">
          <datalist id={listId}>
            {ids.map((id) => (
              <option key={id} value={id} />
            ))}
          </datalist>
          {draft.length === 0 ? (
            <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t('settings.mappingsEmpty')}
            </p>
          ) : (
            <div className="space-y-2">
              <div className="hidden grid-cols-[1fr_20px_1fr_36px] gap-2 px-0.5 text-xs font-medium text-muted-foreground sm:grid">
                <span>{t('settings.mapSource')}</span>
                <span />
                <span>{t('settings.mapTarget')}</span>
                <span />
              </div>
              {draft.map((r) => {
                const e = errors.rows[r.key]
                const known = !r.target.trim() || ids.length === 0 || ids.some((id) => id.toLowerCase() === r.target.trim().toLowerCase())
                return (
                  <div key={r.key} className="grid grid-cols-[1fr_36px] gap-2 sm:grid-cols-[1fr_20px_1fr_36px] sm:items-start">
                    <div className="space-y-1">
                      <Input
                        aria-label={t('settings.mapSource')}
                        className="font-mono text-[13px]"
                        placeholder="claude-sonnet"
                        value={r.source}
                        aria-invalid={!!e?.source}
                        onChange={(ev) => update(r.key, { source: ev.target.value })}
                      />
                      {e?.source && <p className="text-xs text-destructive">{t(e.source as 'settings.mapRequired')}</p>}
                    </div>
                    <ArrowRight className="mt-2.5 hidden size-4 text-muted-foreground sm:block" />
                    <div className="order-3 col-span-1 space-y-1 sm:order-none">
                      <Input
                        aria-label={t('settings.mapTarget')}
                        className="font-mono text-[13px]"
                        placeholder="claude-sonnet-4.5"
                        list={listId}
                        value={r.target}
                        aria-invalid={!!e?.target}
                        onChange={(ev) => update(r.key, { target: ev.target.value })}
                      />
                      {e?.target ? (
                        <p className="text-xs text-destructive">{t(e.target as 'settings.mapRequired')}</p>
                      ) : (
                        !known && <p className="text-xs text-warning">{t('settings.mapUnknownTarget')}</p>
                      )}
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className={cn('text-muted-foreground hover:text-destructive', 'sm:order-none')}
                      aria-label={t('settings.remove')}
                      onClick={() => setDraft((rows) => rows.filter((x) => x.key !== r.key))}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                )
              })}
            </div>
          )}
          <div className="flex items-center justify-between gap-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={draft.length >= MAX_MAPPINGS}
              onClick={() => setDraft((rows) => [...rows, { source: '', target: '', key: newKey() }])}
            >
              <Plus />
              {t('settings.addMapping')}
            </Button>
            <Badge variant="outline" className="tabular text-muted-foreground">
              {draft.length} / {MAX_MAPPINGS}
            </Badge>
          </div>
        </div>
      )}
    </SectionCard>
  )
}
