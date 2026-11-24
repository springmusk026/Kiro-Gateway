import { Loader2, RotateCcw, type LucideIcon } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/**
 * Local editable copy of a server value. Tracks dirtiness and follows the
 * server value while the user has no unsaved edits.
 */
export function useDraft<T>(server: T | undefined) {
  const [base, setBase] = useState<T | undefined>(server)
  const [draft, setDraft] = useState<T | undefined>(server)
  const baseRef = useRef(base)
  const draftRef = useRef(draft)
  baseRef.current = base
  draftRef.current = draft
  const serverKey = JSON.stringify(server)
  useEffect(() => {
    if (server === undefined) return
    if (baseRef.current === undefined || same(baseRef.current, draftRef.current)) {
      setBase(server)
      setDraft(server)
    } else {
      setBase(server)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverKey])
  const dirty = draft !== undefined && base !== undefined && !same(base, draft)
  return {
    draft,
    setDraft: (fn: (d: T) => T) => setDraft((d) => (d === undefined ? d : fn(d))),
    dirty,
    reset: () => setDraft(base),
    /** Mark the current draft as saved (call after a successful save). */
    commit: (v?: T) => {
      const next = v ?? draftRef.current
      setBase(next)
      if (v !== undefined) setDraft(v)
    },
  }
}

/** Run a save and give toast feedback. Returns true on success. */
export async function runSave(fn: () => Promise<unknown>, success: string): Promise<boolean> {
  try {
    await fn()
    toast.success(success)
    return true
  } catch (e) {
    toast.error(e instanceof Error ? e.message : String(e))
    return false
  }
}

export function SectionCard({
  icon: Icon,
  title,
  description,
  children,
  dirty,
  saving,
  onSave,
  onReset,
  saveDisabled,
  error,
  loading,
  footerExtra,
  className,
}: {
  icon: LucideIcon
  title: string
  description: string
  children: ReactNode
  dirty?: boolean
  saving?: boolean
  onSave?: () => void
  onReset?: () => void
  saveDisabled?: boolean
  error?: string | null
  loading?: boolean
  footerExtra?: ReactNode
  className?: string
}) {
  const { t } = useI18n()
  return (
    <Card className={cn('gap-0 overflow-hidden py-0 shadow-xs', className)}>
      <div className="flex items-start gap-3 px-5 pt-5 sm:px-6">
        <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground">
          <Icon className="size-[18px]" />
        </div>
        <div className="min-w-0">
          <h3 className="font-semibold leading-tight">{title}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        </div>
      </div>
      <div className="px-5 py-5 sm:px-6">
        {loading ? (
          <div className="space-y-3">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
          </div>
        ) : (
          children
        )}
      </div>
      {(onSave || footerExtra) && (
        <div
          className={cn(
            'flex flex-wrap items-center gap-3 border-t bg-muted/30 px-5 py-3 sm:px-6',
            dirty && 'bg-primary/[0.04]',
          )}
        >
          <div className="min-w-0 flex-1 text-sm">
            {error ? (
              <span className="text-destructive">{error}</span>
            ) : dirty ? (
              <span className="inline-flex items-center gap-2 font-medium text-primary">
                <span className="size-1.5 rounded-full bg-primary" />
                {t('settings.unsaved')}
              </span>
            ) : (
              footerExtra
            )}
          </div>
          {onSave && (
            <div className="flex items-center gap-2">
              {dirty && onReset && (
                <Button type="button" variant="ghost" size="sm" onClick={onReset} disabled={saving}>
                  <RotateCcw />
                  {t('settings.discard')}
                </Button>
              )}
              <Button type="button" size="sm" onClick={onSave} disabled={!dirty || saving || saveDisabled}>
                {saving && <Loader2 className="animate-spin" />}
                {saving ? t('common.saving') : t('common.save')}
              </Button>
            </div>
          )}
        </div>
      )}
    </Card>
  )
}

/** A label + hint on the left, a control on the right. */
export function SettingRow({
  id,
  label,
  hint,
  children,
  className,
}: {
  id?: string
  label: ReactNode
  hint?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-8', className)}>
      <div className="min-w-0 sm:max-w-[60%]">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
        </label>
        {hint && <div className="mt-0.5 text-[13px] leading-relaxed text-muted-foreground">{hint}</div>}
      </div>
      <div className="shrink-0 sm:pt-0.5">{children}</div>
    </div>
  )
}

export function SectionHeading({ id, title, description }: { id: string; title: string; description: string }) {
  return (
    <div id={id} className="scroll-mt-20 pt-2">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <p className="text-sm text-muted-foreground">{description}</p>
    </div>
  )
}
