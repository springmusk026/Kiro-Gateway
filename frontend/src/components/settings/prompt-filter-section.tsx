import { ArrowDown, ArrowUp, Filter, Plus, Regex, TextSearch, Trash2 } from 'lucide-react'
import { useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { usePromptFilter, useSavePromptFilter } from '@/lib/queries.settings'
import type { PromptFilterConfig, PromptFilterRule } from '@/lib/types'
import { cn } from '@/lib/utils'
import { SectionCard, SettingRow, runSave, useDraft } from './section-card'

const BUILTINS: { key: 'filterClaudeCode' | 'filterEnvNoise' | 'filterStripBoundaries'; label: MessageKey; hint: MessageKey }[] = [
  { key: 'filterClaudeCode', label: 'settings.pfClaudeCode', hint: 'settings.pfClaudeCodeHint' },
  { key: 'filterEnvNoise', label: 'settings.pfEnvNoise', hint: 'settings.pfEnvNoiseHint' },
  { key: 'filterStripBoundaries', label: 'settings.pfBoundaries', hint: 'settings.pfBoundariesHint' },
]

/** Go RE2 is stricter than JS in places (no lookaround/backrefs); JS parse catches the common mistakes. */
function regexError(src: string): string | null {
  if (!src) return null
  // Translate RE2-only syntax JS does not parse: inline flags and (?P<name>…).
  const js = src
    .replace(/\(\?[imsU]+\)/g, '')
    .replace(/\(\?[imsU]*-?[imsU]*:/g, '(?:')
    .replace(/\(\?P</g, '(?<')
  try {
    new RegExp(js, 'u')
  } catch (e) {
    return e instanceof Error ? e.message.replace(/^Invalid regular expression: /, '') : String(e)
  }
  if (/\(\?<?[=!]/.test(src) || /\\[1-9]/.test(src)) return 'RE2'
  return null
}

export function PromptFilterCard() {
  const { t } = useI18n()
  const q = usePromptFilter()
  const save = useSavePromptFilter()
  const { draft, setDraft, dirty, reset, commit } = useDraft<PromptFilterConfig>(q.data)

  const errors = useMemo(() => {
    const out: Record<string, string> = {}
    for (const r of draft?.rules ?? []) {
      if (!r.match.trim()) out[r.id] = t('settings.pfMatchRequired')
      else if (r.type === 'regex') {
        const e = regexError(r.match)
        if (e) out[r.id] = e === 'RE2' ? t('settings.pfRe2') : t('settings.pfInvalidRegex', { msg: e })
      }
    }
    return out
  }, [draft, t])
  const hasErrors = Object.keys(errors).length > 0

  const updateRule = (id: string, patch: Partial<PromptFilterRule>) =>
    setDraft((d) => ({ ...d, rules: d.rules.map((r) => (r.id === id ? { ...r, ...patch } : r)) }))
  const move = (i: number, dir: -1 | 1) =>
    setDraft((d) => {
      const rules = [...d.rules]
      const j = i + dir
      if (j < 0 || j >= rules.length) return d
      ;[rules[i], rules[j]] = [rules[j], rules[i]]
      return { ...d, rules }
    })
  const addRule = (type: PromptFilterRule['type']) =>
    setDraft((d) => ({
      ...d,
      rules: [...d.rules, { id: `rule-${Date.now()}`, name: '', type, match: '', replace: '', enabled: true }],
    }))

  return (
    <SectionCard
      icon={Filter}
      title={t('settings.promptFilter')}
      description={t('settings.promptFilterDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={hasErrors}
      onReset={reset}
      error={dirty && hasErrors ? t('settings.pfFix') : null}
      onSave={async () => {
        if (!draft) return
        const payload: PromptFilterConfig = {
          ...draft,
          rules: draft.rules.map((r) => {
            const base = { id: r.id, name: r.name.trim(), type: r.type, match: r.match, enabled: r.enabled }
            return r.type === 'regex' && r.replace ? { ...base, replace: r.replace } : base
          }),
        }
        if (await runSave(() => save.mutateAsync(payload), t('settings.saved'))) commit()
      }}
    >
      {draft && (
        <div className="space-y-5">
          <div className="space-y-4">
            {BUILTINS.map((b) => (
              <SettingRow key={b.key} id={`pf-${b.key}`} label={t(b.label)} hint={t(b.hint)}>
                <Switch
                  id={`pf-${b.key}`}
                  checked={draft[b.key]}
                  onCheckedChange={(v) => setDraft((d) => ({ ...d, [b.key]: v }))}
                />
              </SettingRow>
            ))}
          </div>
          <Separator />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <p className="text-sm font-medium">{t('settings.pfRules')}</p>
              <p className="text-[13px] text-muted-foreground">{t('settings.pfRulesHint')}</p>
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="outline" size="sm">
                  <Plus />
                  {t('settings.pfAddRule')}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => addRule('regex')}>
                  <Regex />
                  {t('settings.pfTypeRegex')}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => addRule('lines-containing')}>
                  <TextSearch />
                  {t('settings.pfTypeLines')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          {draft.rules.length === 0 ? (
            <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t('settings.pfNoRules')}
            </p>
          ) : (
            <ol className="space-y-3">
              {draft.rules.map((r, i) => (
                <li
                  key={r.id}
                  className={cn('rounded-lg border bg-background p-3 transition-opacity', !r.enabled && 'opacity-70')}
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Switch
                      checked={r.enabled}
                      aria-label={t('common.enabled')}
                      onCheckedChange={(v) => updateRule(r.id, { enabled: v })}
                    />
                    <Badge variant="outline" className="gap-1 font-mono text-[11px]">
                      {r.type === 'regex' ? <Regex /> : <TextSearch />}
                      {r.type === 'regex' ? t('settings.pfTypeRegex') : t('settings.pfTypeLines')}
                    </Badge>
                    <Input
                      aria-label={t('settings.pfName')}
                      placeholder={t('settings.pfNamePlaceholder')}
                      value={r.name}
                      onChange={(e) => updateRule(r.id, { name: e.target.value })}
                      className="h-8 min-w-0 flex-1 basis-40"
                    />
                    <div className="ml-auto flex items-center">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        disabled={i === 0}
                        onClick={() => move(i, -1)}
                        aria-label={t('settings.moveUp')}
                      >
                        <ArrowUp />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        disabled={i === draft.rules.length - 1}
                        onClick={() => move(i, 1)}
                        aria-label={t('settings.moveDown')}
                      >
                        <ArrowDown />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setDraft((d) => ({ ...d, rules: d.rules.filter((x) => x.id !== r.id) }))}
                        aria-label={t('settings.remove')}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </div>
                  <div className={cn('mt-3 grid gap-3', r.type === 'regex' && 'sm:grid-cols-2')}>
                    <div className="space-y-1.5">
                      <Label htmlFor={`pf-m-${r.id}`} className="text-xs text-muted-foreground">
                        {r.type === 'regex' ? t('settings.pfPattern') : t('settings.pfContains')}
                      </Label>
                      <Input
                        id={`pf-m-${r.id}`}
                        className="font-mono text-[13px]"
                        value={r.match}
                        aria-invalid={!!errors[r.id]}
                        placeholder={r.type === 'regex' ? '(?i)some\\s+noise' : '<system-reminder>'}
                        onChange={(e) => updateRule(r.id, { match: e.target.value })}
                      />
                      {errors[r.id] && <p className="text-xs text-destructive">{errors[r.id]}</p>}
                    </div>
                    {r.type === 'regex' && (
                      <div className="space-y-1.5">
                        <Label htmlFor={`pf-r-${r.id}`} className="text-xs text-muted-foreground">
                          {t('settings.pfReplace')}
                        </Label>
                        <Input
                          id={`pf-r-${r.id}`}
                          className="font-mono text-[13px]"
                          value={r.replace ?? ''}
                          placeholder={t('settings.pfReplacePlaceholder')}
                          onChange={(e) => updateRule(r.id, { replace: e.target.value })}
                        />
                      </div>
                    )}
                  </div>
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </SectionCard>
  )
}
