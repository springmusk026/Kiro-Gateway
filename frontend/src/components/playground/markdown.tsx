import { Fragment, type ReactNode } from 'react'
import { CopyButton } from '@/components/app/copy-button'

/** Minimal, XSS-safe Markdown: fenced code, inline code, bold, headings, lists, paragraphs. */
export function Markdown({ text }: { text: string }) {
  const parts = text.split(/(```[\w+-]*\n[\s\S]*?(?:```|$))/g)
  return (
    <div className="space-y-3 text-[14.5px] leading-relaxed">
      {parts.map((part, i) => {
        const fence = part.match(/^```([\w+-]*)\n([\s\S]*?)(?:```)?$/)
        if (fence) {
          return (
            <div key={i} className="group relative overflow-hidden rounded-lg border bg-zinc-950 text-zinc-100">
              <div className="flex items-center justify-between border-b border-white/10 px-3 py-1 text-[11px] text-zinc-400">
                <span>{fence[1] || 'code'}</span>
                <CopyButton value={fence[2]} className="size-6 text-zinc-400 hover:bg-white/10 hover:text-white" />
              </div>
              <pre className="overflow-x-auto p-3 font-mono text-[12.5px]">{fence[2]}</pre>
            </div>
          )
        }
        return <Blocks key={i} text={part} />
      })}
    </div>
  )
}

function Blocks({ text }: { text: string }) {
  const blocks = text.split(/\n{2,}/).filter((b) => b.trim())
  return (
    <>
      {blocks.map((b, i) => {
        const lines = b.split('\n')
        if (lines.every((l) => /^\s*([-*]|\d+\.)\s+/.test(l))) {
          const ordered = /^\s*\d+\./.test(lines[0])
          const items = lines.map((l, j) => <li key={j}>{inline(l.replace(/^\s*([-*]|\d+\.)\s+/, ''))}</li>)
          return ordered ? (
            <ol key={i} className="ml-5 list-decimal space-y-1">{items}</ol>
          ) : (
            <ul key={i} className="ml-5 list-disc space-y-1">{items}</ul>
          )
        }
        const h = b.match(/^(#{1,3})\s+(.*)$/)
        if (h && lines.length === 1) return <p key={i} className="font-semibold">{inline(h[2])}</p>
        return (
          <p key={i} className="whitespace-pre-wrap">
            {lines.map((l, j) => (
              <Fragment key={j}>
                {j > 0 && <br />}
                {inline(l)}
              </Fragment>
            ))}
          </p>
        )
      })}
    </>
  )
}

function inline(s: string): ReactNode[] {
  return s.split(/(`[^`]+`|\*\*[^*]+\*\*)/g).map((tok, i) => {
    if (tok.startsWith('`') && tok.endsWith('`') && tok.length > 1)
      return <code key={i} className="rounded bg-foreground/[0.07] px-1 py-0.5 font-mono text-[0.85em]">{tok.slice(1, -1)}</code>
    if (tok.startsWith('**') && tok.endsWith('**') && tok.length > 3) return <strong key={i}>{tok.slice(2, -2)}</strong>
    return <Fragment key={i}>{tok}</Fragment>
  })
}
