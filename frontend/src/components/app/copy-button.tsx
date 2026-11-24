import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export function CopyButton({ value, className, label }: { value: string; className?: string; label?: string }) {
  const { t } = useI18n()
  const [done, setDone] = useState(false)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size={label ? 'sm' : 'icon'}
          className={cn(!label && 'size-8', className)}
          onClick={async (e) => {
            e.stopPropagation()
            await copyToClipboard(value)
            setDone(true)
            toast.success(t('common.copied'))
            setTimeout(() => setDone(false), 1400)
          }}
        >
          {done ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
          {label}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('common.copy')}</TooltipContent>
    </Tooltip>
  )
}
