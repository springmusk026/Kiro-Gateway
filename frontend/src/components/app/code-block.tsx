import { CopyButton } from './copy-button'
import { cn } from '@/lib/utils'

export function CodeBlock({ code, className }: { code: string; className?: string }) {
  return (
    <div className={cn('group relative min-w-0 max-w-full rounded-lg border bg-zinc-950 text-zinc-100 dark:bg-black/40', className)}>
      <pre className="overflow-x-auto p-4 pr-12 font-mono text-[12.5px] leading-relaxed">{code}</pre>
      <div className="absolute right-2 top-2 opacity-70 transition-opacity group-hover:opacity-100">
        <CopyButton value={code} className="text-zinc-300 hover:bg-white/10 hover:text-white" />
      </div>
    </div>
  )
}

export function InlineCode({ value }: { value: string }) {
  return (
    <div className="flex items-center gap-1 rounded-lg border bg-muted/50 py-1 pl-3 pr-1">
      <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{value}</code>
      <CopyButton value={value} />
    </div>
  )
}
