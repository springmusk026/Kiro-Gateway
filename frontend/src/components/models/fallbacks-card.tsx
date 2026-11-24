import { ArrowRight, LifeBuoy, Plus, Trash2 } from 'lucide-react'
import { useId, useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SectionCard, runSave, useDraft } from '@/components/settings/section-card'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import { useModelFallbacks, useSaveModelFallbacks } from '@/lib/queries.cost'
import type { MessageKey } from '@/locales/en'

type Row = { key: string; model: string; fallbacks: string }
type RowErrors = { model?: MessageKey; fallbacks?: MessageKey }

const MAX_ROWS = 200
const MAX_FALLBACKS = 5
// eslint-disable-next-line no-control-regex
const BAD_ID = /[\s\u0000-\u001f\u007f]/

const split = (s: string) =>
  s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)

function validate(rows: Row[]): Record<string, RowErrors> {
  const out: Record<string, RowErrors> = {}
  const seen = new Set<string>()
  for (const r of rows) {
    const e: RowErrors = {}
    const model = r.model.trim()
    const fbs = split(r.fallbacks)
    if (!model) e.model = 'cost.fbRequired'
    else if (BAD_ID.test(model)) e.model = 'cost.fbInvalid'
    else if (seen.has(model.toLowerCase())) e.model = 'cost.fbDuplicate'
    if (model) seen.add(model.toLowerCase())
    if (!fbs.length) e.fallbacks = 'cost.fbRequired'
    else if (fbs.some((f) => BAD_ID.test(f))) e.fallbacks = 'cost.fbInvalid'
    else if (fbs.some((f) => f.toLowerCase() === model.toLowerCase())) e.fallbacks = 'cost.fbSelf'
    else if (fbs.length > MAX_FALLBACKS) e.fallbacks = 'cost.fbTooMany'
    if (e.model || e.fallbacks) out[r.key] = e
  }
  return out
}

let seq = 0
const newKey = () => `f${Date.now().toString(36)}${seq++}`

/** Ordered fallback models per model (Models → Aliases). */
export function FallbacksCard() {
  const { t } = useI18n()
  const q = useModelFallbacks()
  const models = useModels()
  const save = useSaveModelFallbacks()
  const listId = useId()
  const server = useMemo<Row[] | undefined>(
    () => q.data?.map((f, i) => ({ key: `s${i}`, model: f.model, fallbacks: f.fallbacks.join(', ') })),
    [q.data],
  )
  const { draft, setDraft, dirty, reset, commit } = useDraft<Row[]>(server)
  const errors = useMemo(() => (draft ? validate(draft) : {}), [draft])
  const hasErrors = Object.keys(errors).length > 0
  const ids = models.data?.models.map((m) => m.id) ?? []
  const known = (id: string) => !id || ids.length === 0 || ids.some((x) => x.toLowerCase() === id.toLowerCase())
  const update = (key: string, patch: Partial<Row>) => setDraft((rows) => rows.map((r) => (r.key === key ? { ...r, ...patch } : r)))

  return (
    <SectionCard
      icon={LifeBuoy}
      title={t('cost.fallbacks')}
      description={t('cost.fallbacksDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={hasErrors}
      onReset={reset}
      error={dirty && hasErrors ? t('cost.fbFix') : null}
      onSave={async () => {
        if (!draft) return
        const payload = draft.map((r) => ({ model: r.model.trim(), fallbacks: split(r.fallbacks) }))
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
            <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">{t('cost.fallbacksEmpty')}</p>
          ) : (
            <div className="space-y-2">
              <div className="hidden grid-cols-[1fr_20px_1.4fr_36px] gap-2 px-0.5 text-xs font-medium text-muted-foreground sm:grid">
                <span>{t('cost.fbModel')}</span>
                <span />
                <span>{t('cost.fbFallbacks')}</span>
                <span />
              </div>
              {draft.map((r) => {
                const e = errors[r.key]
                const unknown = split(r.fallbacks).filter((f) => !known(f))
                return (
                  <div key={r.key} className="grid grid-cols-[1fr_36px] gap-2 sm:grid-cols-[1fr_20px_1.4fr_36px] sm:items-start">
                    <div className="space-y-1">
                      <Input
                        aria-label={t('cost.fbModel')}
                        className="font-mono text-[13px]"
                        list={listId}
                        value={r.model}
                        aria-invalid={!!e?.model}
                        onChange={(ev) => update(r.key, { model: ev.target.value })}
                      />
                      {e?.model ? (
                        <p className="text-xs text-destructive">{t(e.model)}</p>
                      ) : (
                        !known(r.model.trim()) && <p className="text-xs text-warning">{t('cost.fbUnknown')}</p>
                      )}
                    </div>
                    <ArrowRight className="mt-2.5 hidden size-4 text-muted-foreground sm:block" />
                    <div className="order-3 space-y-1 sm:order-none">
                      <Input
                        aria-label={t('cost.fbFallbacks')}
                        className="font-mono text-[13px]"
                        placeholder={t('cost.fbFallbacksPlaceholder')}
                        list={listId}
                        value={r.fallbacks}
                        aria-invalid={!!e?.fallbacks}
                        onChange={(ev) => update(r.key, { fallbacks: ev.target.value })}
                      />
                      {e?.fallbacks ? (
                        <p className="text-xs text-destructive">{t(e.fallbacks)}</p>
                      ) : (
                        unknown.length > 0 && (
                          <p className="text-xs text-warning">
                            {t('cost.fbUnknown')}: <span className="font-mono">{unknown.join(', ')}</span>
                          </p>
                        )
                      )}
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="text-muted-foreground hover:text-destructive"
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
              disabled={draft.length >= MAX_ROWS}
              onClick={() => setDraft((rows) => [...rows, { key: newKey(), model: '', fallbacks: '' }])}
            >
              <Plus />
              {t('cost.fbAdd')}
            </Button>
            <Badge variant="outline" className="tabular text-muted-foreground">
              {draft.length} / {MAX_ROWS}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground">{t('cost.fbHint')}</p>
        </div>
      )}
    </SectionCard>
  )
}
