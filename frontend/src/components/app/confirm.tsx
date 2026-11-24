import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/ui/button'

type Options = { title: string; description?: string; confirmLabel?: string; destructive?: boolean }
const Ctx = createContext<(o: Options) => Promise<boolean>>(async () => false)

/** Promise-based confirmation dialog: `if (await confirm({...})) …` */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n()
  const [opts, setOpts] = useState<Options | null>(null)
  const resolver = useRef<(v: boolean) => void>(() => {})
  const confirm = useCallback((o: Options) => {
    setOpts(o)
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve
    })
  }, [])
  const close = (v: boolean) => {
    resolver.current(v)
    setOpts(null)
  }
  return (
    <Ctx.Provider value={confirm}>
      {children}
      <AlertDialog open={!!opts} onOpenChange={(o) => !o && close(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{opts?.title}</AlertDialogTitle>
            {opts?.description && <AlertDialogDescription>{opts.description}</AlertDialogDescription>}
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => close(false)}>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className={cn(opts?.destructive && buttonVariants({ variant: 'destructive' }))}
              onClick={() => close(true)}
            >
              {opts?.confirmLabel ?? t('common.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Ctx.Provider>
  )
}

export const useConfirm = () => useContext(Ctx)
