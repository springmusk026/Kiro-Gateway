import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useI18n } from '@/lib/i18n'
import { useAccounts } from '@/lib/queries.accounts'
import type { ApiKeyView } from '@/lib/types'
import type { MessageKey } from '@/locales/en'

/** Routing rules of the key dialog: pinned accounts and a model map. */
export type RoutingForm = { accountIds: string[]; modelMap: string }

export const blankRouting: RoutingForm = { accountIds: [], modelMap: '' }

export function routingFromKey(k: ApiKeyView | null): RoutingForm {
  if (!k) return blankRouting
  return {
    accountIds: k.accountIds ?? [],
    modelMap: Object.entries(k.modelMap ?? {})
      .map(([from, to]) => `${from} = ${to}`)
      .join('\n'),
  }
}

/** Parses "client-model = model" lines; null when a line is malformed. */
function parseModelMap(text: string): Record<string, string> | null {
  const out: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    const i = line.indexOf('=')
    const from = i < 0 ? '' : line.slice(0, i).trim()
    const to = i < 0 ? '' : line.slice(i + 1).trim()
    if (!from || !to) return null
    out[from] = to
  }
  return out
}

export function routingError(f: RoutingForm, t: (k: MessageKey) => string): string | null {
  return parseModelMap(f.modelMap) ? null : t('keyroute.mapError')
}

/** The wire fields for create / update. Empty values clear a rule. */
export function routingBody(f: RoutingForm) {
  return { accountIds: f.accountIds, modelMap: parseModelMap(f.modelMap) ?? {} }
}

export function KeyRoutingFields({ value, onChange }: { value: RoutingForm; onChange: (v: RoutingForm) => void }) {
  const { t } = useI18n()
  const accounts = useAccounts()
  const selected = new Set(value.accountIds)
  const toggle = (id: string, on: boolean) =>
    onChange({ ...value, accountIds: on ? [...value.accountIds, id] : value.accountIds.filter((v) => v !== id) })
  const mapErr = routingError(value, t)

  return (
    <fieldset className="grid gap-3 rounded-lg border p-3">
      <legend className="px-1 text-sm font-medium">{t('keyroute.title')}</legend>
      <div className="space-y-2">
        <Label>{t('keyroute.accounts')}</Label>
        <div className="max-h-36 space-y-1.5 overflow-y-auto">
          {(accounts.data ?? []).map((a) => (
            <label key={a.id} className="flex min-w-0 items-center gap-2 text-sm">
              <Checkbox checked={selected.has(a.id)} onCheckedChange={(v) => toggle(a.id, v === true)} />
              <span className="truncate">{a.nickname || a.email || a.id}</span>
            </label>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">{t('keyroute.accountsHint')}</p>
      </div>
      <div className="space-y-2">
        <Label htmlFor="k-model-map">{t('keyroute.modelMap')}</Label>
        <Textarea
          id="k-model-map"
          rows={2}
          className="font-mono text-[13px]"
          placeholder="gpt-4o = claude-sonnet-4.5"
          spellCheck={false}
          value={value.modelMap}
          aria-invalid={!!mapErr}
          onChange={(e) => onChange({ ...value, modelMap: e.target.value })}
        />
        <p className={mapErr ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{mapErr ?? t('keyroute.modelMapHint')}</p>
      </div>
    </fieldset>
  )
}
