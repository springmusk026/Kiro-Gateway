import { Check, ChevronsUpDown, Plus, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import type { ApiKeyView } from '@/lib/types'
import type { MessageKey } from '@/locales/en'
import { cn } from '@/lib/utils'

/** Restriction fields of the key dialog: allowed models, RPM and expiry. */
export type RestrictionsForm = { allowedModels: string[]; rpm: string; expires: boolean; expiryDate: string }

export const blankRestrictions: RestrictionsForm = { allowedModels: [], rpm: '', expires: false, expiryDate: '' }

function toDateInput(unix: number): string {
  const d = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** End of the chosen day, local time, as Unix seconds. */
function fromDateInput(v: string): number {
  const [y, m, d] = v.split('-').map(Number)
  if (!y || !m || !d) return 0
  return Math.floor(new Date(y, m - 1, d, 23, 59, 59).getTime() / 1000)
}

export function restrictionsFromKey(k: ApiKeyView | null): RestrictionsForm {
  if (!k) return blankRestrictions
  return {
    allowedModels: k.allowedModels ?? [],
    rpm: k.rpmLimit ? String(k.rpmLimit) : '',
    expires: !!k.expiresAt,
    expiryDate: k.expiresAt ? toDateInput(k.expiresAt) : '',
  }
}

export function restrictionsError(f: RestrictionsForm, t: (k: MessageKey) => string): string | null {
  if (f.rpm !== '' && !/^\d+$/.test(f.rpm.trim())) return t('keysec.rpmError')
  if (f.expires && !fromDateInput(f.expiryDate)) return t('keysec.dateError')
  return null
}

/** The wire fields for create / update. Empty values clear a restriction. */
export function restrictionsBody(f: RestrictionsForm) {
  return {
    allowedModels: f.allowedModels,
    rpmLimit: f.rpm ? parseInt(f.rpm, 10) : 0,
    expiresAt: f.expires ? fromDateInput(f.expiryDate) : 0,
  }
}

function ModelPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const { t } = useI18n()
  const models = useModels()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const options = useMemo(() => {
    const ids = new Set<string>()
    for (const m of models.data?.models ?? []) ids.add(m.id.toLowerCase())
    for (const m of models.data?.mappings ?? []) ids.add(m.source.toLowerCase())
    return [...ids].sort()
  }, [models.data])
  const selected = new Set(value)
  const toggle = (id: string) => onChange(selected.has(id) ? value.filter((v) => v !== id) : [...value, id])
  const typed = search.trim().toLowerCase()
  const canAdd = typed !== '' && !selected.has(typed) && !options.includes(typed) && /^[a-z0-9._:/-]+\*?$/.test(typed)

  return (
    <div className="space-y-2">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button type="button" variant="outline" role="combobox" aria-expanded={open} id="k-models" className="w-full justify-between font-normal">
            <span className={cn('truncate', value.length === 0 && 'text-muted-foreground')}>
              {value.length === 0 ? t('keysec.allModels') : t('keysec.modelsSelected', { n: value.length })}
            </span>
            <ChevronsUpDown className="opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] p-0" align="start">
          <Command>
            <CommandInput placeholder={t('keysec.searchModels')} value={search} onValueChange={setSearch} />
            <CommandList>
              <CommandEmpty>{t('keysec.noModels')}</CommandEmpty>
              {canAdd && (
                <CommandGroup>
                  <CommandItem
                    value={`add:${typed}`}
                    onSelect={() => {
                      onChange([...value, typed])
                      setSearch('')
                    }}
                  >
                    <Plus />
                    {t('keysec.addPattern', { pattern: typed })}
                  </CommandItem>
                </CommandGroup>
              )}
              <CommandGroup>
                {options.map((id) => (
                  <CommandItem key={id} value={id} onSelect={() => toggle(id)}>
                    <Check className={cn(selected.has(id) ? 'opacity-100' : 'opacity-0')} />
                    <span className="font-mono text-[13px]">{id}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {value.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1 pr-1 font-mono text-[12px]">
              {id}
              <button
                type="button"
                className="rounded-sm p-0.5 hover:bg-foreground/10"
                aria-label={t('keysec.removeModel', { model: id })}
                onClick={() => toggle(id)}
              >
                <X className="size-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}

/** Restriction inputs, rendered as one block inside the key dialog. */
export function KeyRestrictionsFields({ value: f, onChange }: { value: RestrictionsForm; onChange: (v: RestrictionsForm) => void }) {
  const { t } = useI18n()
  const rpmErr = f.rpm !== '' && !/^\d+$/.test(f.rpm.trim())
  return (
    <fieldset className="grid gap-4 rounded-lg border p-3">
      <legend className="px-1 text-sm font-medium">{t('keysec.title')}</legend>
      <div className="space-y-2">
        <Label htmlFor="k-models">{t('keysec.allowedModels')}</Label>
        <ModelPicker value={f.allowedModels} onChange={(allowedModels) => onChange({ ...f, allowedModels })} />
        <p className="text-xs text-muted-foreground">{t('keysec.allowedModelsHint')}</p>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-2">
          <Label htmlFor="k-rpm">{t('keysec.rpm')}</Label>
          <Input
            id="k-rpm"
            inputMode="numeric"
            className="tabular"
            placeholder={t('keys.unlimited')}
            value={f.rpm}
            aria-invalid={rpmErr}
            onChange={(e) => onChange({ ...f, rpm: e.target.value })}
          />
          {rpmErr && <p className="text-xs text-destructive">{t('keysec.rpmError')}</p>}
        </div>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Label htmlFor="k-expiry">{t('keysec.expiry')}</Label>
            <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Switch
                checked={f.expires}
                aria-label={t('keysec.expires')}
                onCheckedChange={(v) => onChange({ ...f, expires: v, expiryDate: v && !f.expiryDate ? toDateInput(Date.now() / 1000 + 30 * 86400) : f.expiryDate })}
              />
              {t('keysec.expires')}
            </label>
          </div>
          {f.expires ? (
            <Input id="k-expiry" type="date" value={f.expiryDate} onChange={(e) => onChange({ ...f, expiryDate: e.target.value })} />
          ) : (
            <p id="k-expiry" className="flex h-9 items-center rounded-md border bg-muted/50 px-3 text-sm text-muted-foreground">
              {t('keysec.never')}
            </p>
          )}
        </div>
      </div>
    </fieldset>
  )
}
