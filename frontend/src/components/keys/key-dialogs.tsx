import { AlertTriangle, Check, Copy, KeyRound, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { copyToClipboard } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useCreateApiKey, useUpdateApiKey, type ApiKeyInput } from '@/lib/queries.settings'
import type { ApiKeyView } from '@/lib/types'
import { KeyRestrictionsFields, blankRestrictions, restrictionsBody, restrictionsError, restrictionsFromKey, type RestrictionsForm } from './key-restrictions'

type Form = { name: string; key: string; enabled: boolean; tokenLimit: string; creditLimit: string }
const blank: Form = { name: '', key: '', enabled: true, tokenLimit: '', creditLimit: '' }

/** Create (editing = null) or edit an API key. */
export function KeyDialog({
  open,
  onOpenChange,
  editing,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: ApiKeyView | null
  onCreated: (plaintext: string, view: ApiKeyView) => void
}) {
  const { t } = useI18n()
  const create = useCreateApiKey()
  const update = useUpdateApiKey()
  const [f, setF] = useState<Form>(blank)
  const [error, setError] = useState<string | null>(null)
  // Access restrictions (allowed models, RPM, expiry) — kept in their own state block.
  const [restrict, setRestrict] = useState<RestrictionsForm>(blankRestrictions)
  useEffect(() => {
    if (open) setRestrict(restrictionsFromKey(editing))
  }, [open, editing])
  const restrictErr = restrictionsError(restrict, t)
  useEffect(() => {
    if (!open) return
    setError(null)
    setF(
      editing
        ? {
            name: editing.name ?? '',
            key: '',
            enabled: editing.enabled,
            tokenLimit: editing.tokenLimit ? String(editing.tokenLimit) : '',
            creditLimit: editing.creditLimit ? String(editing.creditLimit) : '',
          }
        : blank,
    )
  }, [open, editing])

  const tokErr = f.tokenLimit !== '' && !/^\d+$/.test(f.tokenLimit.trim()) ? t('keys.limitIntError') : null
  const credErr =
    f.creditLimit !== '' && (!/^\d*\.?\d+$/.test(f.creditLimit.trim()) || Number.isNaN(+f.creditLimit)) ? t('keys.limitNumError') : null
  const keyErr = f.key !== '' && /\s/.test(f.key) ? t('keys.keyWhitespace') : f.key !== '' && f.key.length < 8 ? t('keys.keyShort') : null
  const pending = create.isPending || update.isPending
  const invalid = !!(tokErr || credErr || keyErr || restrictErr)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (invalid || pending) return
    setError(null)
    const body: ApiKeyInput = {
      name: f.name.trim(),
      enabled: f.enabled,
      tokenLimit: f.tokenLimit ? parseInt(f.tokenLimit, 10) : 0,
      creditLimit: f.creditLimit ? parseFloat(f.creditLimit) : 0,
      ...restrictionsBody(restrict),
    }
    try {
      if (editing) {
        await update.mutateAsync({ id: editing.id, patch: body })
        toast.success(t('keys.updated'))
        onOpenChange(false)
      } else {
        const r = await create.mutateAsync({ ...body, ...(f.key.trim() ? { key: f.key.trim() } : {}) })
        onOpenChange(false)
        onCreated(r.key, r.apiKey)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="grid gap-5">
          <DialogHeader>
            <DialogTitle>{editing ? t('keys.editTitle') : t('keys.createTitle')}</DialogTitle>
            <DialogDescription>{editing ? t('keys.editDesc') : t('keys.createDesc')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="k-name">{t('keys.name')}</Label>
            <Input
              id="k-name"
              autoFocus
              placeholder={t('keys.namePlaceholder')}
              value={f.name}
              onChange={(e) => setF({ ...f, name: e.target.value })}
            />
          </div>
          {editing ? (
            <div className="space-y-2">
              <Label>{t('keys.key')}</Label>
              <p className="rounded-md border bg-muted/50 px-3 py-2 font-mono text-[13px] text-muted-foreground">{editing.keyMasked}</p>
            </div>
          ) : (
            <div className="space-y-2">
              <Label htmlFor="k-key">
                {t('keys.customKey')} <span className="font-normal text-muted-foreground">({t('common.optional')})</span>
              </Label>
              <Input
                id="k-key"
                className="font-mono text-[13px]"
                placeholder={t('keys.customKeyPlaceholder')}
                autoComplete="off"
                spellCheck={false}
                value={f.key}
                aria-invalid={!!keyErr}
                onChange={(e) => setF({ ...f, key: e.target.value })}
              />
              {keyErr && <p className="text-xs text-destructive">{keyErr}</p>}
            </div>
          )}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor="k-tok">{t('keys.tokenLimit')}</Label>
              <Input
                id="k-tok"
                inputMode="numeric"
                className="tabular"
                placeholder={t('keys.unlimited')}
                value={f.tokenLimit}
                aria-invalid={!!tokErr}
                onChange={(e) => setF({ ...f, tokenLimit: e.target.value })}
              />
              {tokErr && <p className="text-xs text-destructive">{tokErr}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="k-cred">{t('keys.creditLimit')}</Label>
              <Input
                id="k-cred"
                inputMode="decimal"
                className="tabular"
                placeholder={t('keys.unlimited')}
                value={f.creditLimit}
                aria-invalid={!!credErr}
                onChange={(e) => setF({ ...f, creditLimit: e.target.value })}
              />
              {credErr && <p className="text-xs text-destructive">{credErr}</p>}
            </div>
            <p className="col-span-2 -mt-1 text-xs text-muted-foreground">{t('keys.limitHint')}</p>
          </div>
          {/* Access restrictions */}
          <KeyRestrictionsFields value={restrict} onChange={setRestrict} />
          <div className="flex items-center justify-between rounded-lg border px-3 py-2.5">
            <Label htmlFor="k-enabled">{t('common.enabled')}</Label>
            <Switch id="k-enabled" checked={f.enabled} onCheckedChange={(v) => setF({ ...f, enabled: v })} />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={invalid || pending}>
              {pending && <Loader2 className="animate-spin" />}
              {editing ? t('common.save') : t('keys.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Shows a newly created key exactly once. */
export function RevealDialog({ value, onClose }: { value: string | null; onClose: () => void }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  useEffect(() => setCopied(false), [value])
  return (
    <Dialog open={!!value} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="size-5 text-primary" />
            {t('keys.revealTitle')}
          </DialogTitle>
          <DialogDescription>{t('keys.revealDesc')}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 rounded-lg border bg-muted/50 p-2 pl-3">
          <code className="min-w-0 flex-1 break-all font-mono text-[13px] select-all">{value}</code>
          <Button
            size="sm"
            onClick={async () => {
              if (!value) return
              await copyToClipboard(value)
              setCopied(true)
              toast.success(t('common.copied'))
            }}
          >
            {copied ? <Check /> : <Copy />}
            {copied ? t('keys.copied') : t('common.copy')}
          </Button>
        </div>
        <Alert className="border-warning/40 bg-warning/10">
          <AlertTriangle className="text-warning" />
          <AlertDescription className="text-foreground/80">{t('keys.revealWarning')}</AlertDescription>
        </Alert>
        <DialogFooter>
          <Button variant={copied ? 'default' : 'outline'} onClick={onClose}>
            {copied ? t('common.done') : t('keys.revealDone')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
