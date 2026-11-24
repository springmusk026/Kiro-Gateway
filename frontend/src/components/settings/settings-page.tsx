import { Database, Filter, Globe, Route, ShieldCheck, Wrench, type LucideIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { PageHeader } from '@/components/app/page-header'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { cn } from '@/lib/utils'
import { LoggingCard, StorageCard } from './logging-section'
import { AboutCard, ResetStatsCard } from './maintenance-section'
import { ProxyCard } from './network-section'
import { PromptFilterCard } from './prompt-filter-section'
import { EndpointCard, UsageCard } from './routing-section'
import { SectionHeading } from './section-card'
import { ApiAccessCard, PasswordCard } from './security-section'

type Section = { id: string; icon: LucideIcon; title: MessageKey; desc: MessageKey; cards: React.FC[] }
const SECTIONS: Section[] = [
  { id: 'security', icon: ShieldCheck, title: 'settings.secSecurity', desc: 'settings.secSecurityDesc', cards: [ApiAccessCard, PasswordCard] },
  { id: 'routing', icon: Route, title: 'settings.secRouting', desc: 'settings.secRoutingDesc', cards: [EndpointCard, UsageCard] },
  { id: 'prompt-filter', icon: Filter, title: 'settings.secFilter', desc: 'settings.secFilterDesc', cards: [PromptFilterCard] },
  { id: 'network', icon: Globe, title: 'settings.secNetwork', desc: 'settings.secNetworkDesc', cards: [ProxyCard] },
  { id: 'logging', icon: Database, title: 'settings.secLogging', desc: 'settings.secLoggingDesc', cards: [StorageCard, LoggingCard] },
  { id: 'maintenance', icon: Wrench, title: 'settings.secMaintenance', desc: 'settings.secMaintenanceDesc', cards: [ResetStatsCard, AboutCard] },
]

function useActiveSection(ids: string[]) {
  const [active, setActive] = useState(ids[0])
  useEffect(() => {
    const els = ids.map((id) => document.getElementById(id)).filter(Boolean) as HTMLElement[]
    const obs = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible[0]) setActive(visible[0].target.id.replace(/^sec-/, ''))
      },
      { rootMargin: '-80px 0px -65% 0px' },
    )
    els.forEach((el) => obs.observe(el))
    return () => obs.disconnect()
  }, [ids])
  return [active, setActive] as const
}

const IDS = SECTIONS.map((s) => 'sec-' + s.id)

export function SettingsPage() {
  const { t } = useI18n()
  const [active, setActive] = useActiveSection(IDS)
  const go = (id: string) => {
    setActive(id)
    const el = document.getElementById('sec-' + id)
    if (!el) return
    const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth', block: 'start' })
  }
  // Deep links such as /settings#security.
  useEffect(() => {
    const h = window.location.hash.replace(/^#(sec-)?/, '')
    if (h === 'models') {
      window.location.replace(window.location.pathname.replace(/settings\/?$/, 'models') + '?tab=aliases')
      return
    }
    if (h && SECTIONS.some((s) => s.id === h)) setTimeout(() => go(h), 50)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return (
    <div className="space-y-6">
      <PageHeader title={t('nav.settings')} description={t('page.settings')} />
      <nav className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 lg:hidden" aria-label={t('settings.sections')}>
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => go(s.id)}
            className={cn(
              'flex h-8 shrink-0 items-center gap-1.5 rounded-full border px-3 text-xs font-medium text-muted-foreground',
              active === s.id && 'border-primary/40 bg-primary/[0.06] text-foreground',
            )}
          >
            <s.icon className="size-3.5" />
            {t(s.title)}
          </button>
        ))}
      </nav>
      <div className="lg:grid lg:grid-cols-[200px_minmax(0,1fr)] lg:gap-10">
        <aside className="hidden lg:block">
          <nav className="sticky top-20 flex flex-col gap-0.5" aria-label={t('settings.sections')}>
            {SECTIONS.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => go(s.id)}
                aria-current={active === s.id ? 'true' : undefined}
                className={cn(
                  'relative flex h-9 items-center gap-2.5 rounded-lg px-3 text-left text-sm text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground',
                  active === s.id && 'bg-accent font-medium text-foreground',
                )}
              >
                <span
                  className={cn(
                    'absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-full bg-primary transition-opacity',
                    active === s.id ? 'opacity-100' : 'opacity-0',
                  )}
                />
                <s.icon className={cn('size-4', active === s.id && 'text-primary')} />
                {t(s.title)}
              </button>
            ))}
          </nav>
        </aside>
        <div className="min-w-0 space-y-10">
          {SECTIONS.map((s) => (
            <section key={s.id} className="space-y-4" aria-labelledby={'sec-' + s.id}>
              <SectionHeading id={'sec-' + s.id} title={t(s.title)} description={t(s.desc)} />
              <div className="stagger space-y-4">
                {s.cards.map((C, i) => (
                  <C key={i} />
                ))}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}
