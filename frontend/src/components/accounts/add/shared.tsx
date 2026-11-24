import { AlertCircle, ExternalLink, FileUp, Lightbulb } from 'lucide-react'
import { useRef, type ReactNode } from 'react'
import { toast } from 'sonner'
import { CopyButton } from '@/components/app/copy-button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useI18n } from '@/lib/i18n'
import type { NewAccountRef } from '@/lib/types'
import { cn } from '@/lib/utils'
import { REGIONS, type ErrorInfo } from '../lib'

export interface FlowProps {
  /** Return to the method picker. */
  onBack: () => void
  /** Accounts were created: refresh list, highlight, auto-refresh info. Does not close. */
  onAdded: (refs: NewAccountRef[]) => void
  /** Close the whole dialog. */
  onClose: () => void
  /** Convenience: onAdded + success toast + close. */
  onSuccess: (refs: NewAccountRef[], title: string, warning?: string) => void
}

export function ErrorCallout({ error, className }: { error: ErrorInfo | null; className?: string }) {
  const { t } = useI18n()
  if (!error) return null
  return (
    <Alert variant="destructive" className={cn('animate-rise', className)}>
      <AlertCircle />
      <AlertTitle>{error.status === 409 ? t('accounts.add.duplicate') : t('accounts.add.failed')}</AlertTitle>
      <AlertDescription className="space-y-2">
        <p className="break-words">{error.message}</p>
        {error.hint && <p className="text-foreground/80">{error.hint}</p>}
        {error.rotatedRefreshToken && (
          <div className="w-full space-y-1">
            <p className="text-xs text-foreground/80">{t('accounts.add.rotatedToken')}</p>
            <div className="flex items-center gap-1 rounded-md border bg-background py-1 pl-2 pr-1 text-foreground">
              <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{error.rotatedRefreshToken}</code>
              <CopyButton value={error.rotatedRefreshToken} className="size-7" />
            </div>
          </div>
        )}
      </AlertDescription>
    </Alert>
  )
}

export function UrlBox({ url, label }: { url: string; label: string }) {
  const { t } = useI18n()
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <div className="flex items-center gap-1 rounded-lg border bg-muted/40 py-1 pl-3 pr-1">
        <code className="min-w-0 flex-1 truncate font-mono text-xs" title={url}>
          {url}
        </code>
        <CopyButton value={url} />
        <Button size="sm" asChild>
          <a href={url} target="_blank" rel="noopener noreferrer">
            <ExternalLink /> {t('accounts.add.open')}
          </a>
        </Button>
      </div>
    </div>
  )
}

export function RegionField({ id, value, onChange, hint }: { id: string; value: string; onChange: (v: string) => void; hint?: string }) {
  const { t } = useI18n()
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{t('accounts.add.region')}</Label>
      <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} list={`${id}-list`} className="font-mono text-sm" placeholder="us-east-1" />
      <datalist id={`${id}-list`}>
        {REGIONS.map((r) => (
          <option key={r} value={r} />
        ))}
      </datalist>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

/** Textarea with an "Upload .json" button that fills it from a local file. */
export function FileTextarea({
  id,
  label,
  hint,
  value,
  onChange,
  placeholder,
  rows = 5,
  accept = '.json,application/json',
  required,
}: {
  id: string
  label: ReactNode
  hint?: ReactNode
  value: string
  onChange: (v: string) => void
  placeholder?: string
  rows?: number
  accept?: string
  required?: boolean
}) {
  const { t } = useI18n()
  const fileRef = useRef<HTMLInputElement>(null)
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={id}>
          {label}
          {required && <span className="text-destructive">*</span>}
        </Label>
        <Button type="button" size="xs" variant="outline" onClick={() => fileRef.current?.click()}>
          <FileUp /> {t('accounts.add.upload')}
        </Button>
        <input
          ref={fileRef}
          type="file"
          accept={accept}
          className="hidden"
          onChange={async (e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (!f) return
            try {
              onChange(await f.text())
            } catch {
              toast.error(t('accounts.add.readFailed'))
            }
          }}
        />
      </div>
      <Textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        rows={rows}
        spellCheck={false}
        className="max-h-64 font-mono text-xs"
      />
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function HelpSteps({ title, steps }: { title: string; steps: ReactNode[] }) {
  return (
    <div className="rounded-lg border border-info/25 bg-info/5 p-3 text-sm">
      <p className="flex items-center gap-1.5 font-medium text-info">
        <Lightbulb className="size-4" /> {title}
      </p>
      <ol className="mt-1.5 list-decimal space-y-1 pl-5 text-muted-foreground">
        {steps.map((s, i) => (
          <li key={i}>{s}</li>
        ))}
      </ol>
    </div>
  )
}

export function Code({ children }: { children: ReactNode }) {
  return <code className="rounded bg-muted px-1 py-0.5 font-mono text-[12px] text-foreground">{children}</code>
}

export function FlowFooter({ children }: { children: ReactNode }) {
  return <div className="flex flex-col-reverse gap-2 border-t pt-4 sm:flex-row sm:justify-end">{children}</div>
}
