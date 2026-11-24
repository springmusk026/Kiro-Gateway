import { cn } from '@/lib/utils'

/** Brand mark: a gradient tile with a routing "K" made of a stem and two paths. */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn('shrink-0', className)} aria-hidden>
      <defs>
        <linearGradient id="kg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="oklch(0.62 0.21 285)" />
          <stop offset="100%" stopColor="oklch(0.55 0.2 250)" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="9" fill="url(#kg)" />
      <path d="M11 8.5v15" stroke="white" strokeWidth="2.6" strokeLinecap="round" />
      <path d="M21.5 8.5 13.5 16l8 7.5" stroke="white" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" fill="none" />
      <circle cx="21.5" cy="8.5" r="2" fill="white" />
      <circle cx="21.5" cy="23.5" r="2" fill="white" opacity="0.75" />
    </svg>
  )
}
