import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import {
  Boxes,
  ChartColumn,
  FlaskConical,
  Gauge,
  KeyRound,
  Languages,
  LogOut,
  Menu,
  Monitor,
  Moon,
  ScrollText,
  Search,
  Settings,
  Sun,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { MigrationPrompt } from '@/components/app/migration'
import { Button } from '@/components/ui/button'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useAuth } from '@/lib/auth'
import { LANGS, useI18n, type Lang } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { useStatus } from '@/lib/queries'
import { useTheme, type Theme } from '@/lib/theme'
import { cn } from '@/lib/utils'
import { Logo } from './logo'

type NavItem = { to: string; icon: LucideIcon; label: MessageKey; desc: MessageKey }
const NAV: { section: MessageKey; items: NavItem[] }[] = [
  {
    section: 'nav.workspace',
    items: [
      { to: '/', icon: Gauge, label: 'nav.overview', desc: 'page.overview' },
      { to: '/analytics', icon: ChartColumn, label: 'nav.analytics', desc: 'page.analytics' },
      { to: '/accounts', icon: Users, label: 'nav.accounts', desc: 'page.accounts' },
      { to: '/models', icon: Boxes, label: 'nav.models', desc: 'page.models' },
      { to: '/playground', icon: FlaskConical, label: 'nav.playground', desc: 'page.playground' },
    ],
  },
  {
    section: 'nav.developers',
    items: [
      { to: '/keys', icon: KeyRound, label: 'nav.keys', desc: 'page.keys' },
      { to: '/logs', icon: ScrollText, label: 'nav.logs', desc: 'page.logs' },
    ],
  },
  { section: 'nav.system', items: [{ to: '/settings', icon: Settings, label: 'nav.settings', desc: 'page.settings' }] },
]

function useActivePath() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const p = pathname.replace(/^\/admin/, '') || '/'
  return p === '' ? '/' : p
}

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useI18n()
  const active = useActivePath()
  return (
    <nav className="flex flex-col gap-5">
      {NAV.map((group) => (
        <div key={group.section}>
          <p className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/80">
            {t(group.section)}
          </p>
          <ul className="flex flex-col gap-0.5">
            {group.items.map((item) => {
              const isActive = item.to === '/' ? active === '/' : active.startsWith(item.to)
              return (
                <li key={item.to}>
                  <Link
                    to={item.to}
                    onClick={onNavigate}
                    className={cn(
                      'group relative flex h-9 items-center gap-3 rounded-lg px-3 text-sm font-medium text-sidebar-foreground/75 transition-colors',
                      'hover:bg-sidebar-accent/70 hover:text-sidebar-foreground',
                      isActive && 'bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent',
                    )}
                  >
                    <span
                      className={cn(
                        'absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-full bg-sidebar-primary transition-all duration-300',
                        isActive ? 'opacity-100' : 'h-0 opacity-0',
                      )}
                    />
                    <item.icon className={cn('size-[17px] transition-transform group-hover:scale-110', isActive && 'text-sidebar-primary')} />
                    {t(item.label)}
                  </Link>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

function StatusPill() {
  const { t } = useI18n()
  const { data, isError } = useStatus()
  const state = isError ? 'offline' : !data ? 'loading' : data.available > 0 ? 'healthy' : 'degraded'
  const label =
    state === 'offline' ? t('status.offline') : state === 'degraded' ? t('status.degraded') : t('status.healthy')
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="hidden items-center gap-2 rounded-full border bg-card px-3 py-1 text-xs font-medium sm:flex">
          <span
            className={cn(
              'size-2 rounded-full',
              state === 'healthy' && 'animate-pulse-ring bg-success text-success',
              state === 'degraded' && 'bg-warning',
              state === 'offline' && 'bg-destructive',
              state === 'loading' && 'bg-muted-foreground/40',
            )}
          />
          <span className="text-muted-foreground">{state === 'loading' ? t('common.loading') : label}</span>
        </div>
      </TooltipTrigger>
      {data && <TooltipContent>{t('status.accounts', { available: data.available, total: data.accounts })}</TooltipContent>}
    </Tooltip>
  )
}

function ThemeMenu() {
  const { t } = useI18n()
  const { theme, setTheme, resolved } = useTheme()
  const Icon = resolved === 'dark' ? Moon : Sun
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-9" aria-label={t('common.theme')}>
          <Icon className="size-[18px]" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuLabel>{t('common.theme')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme} onValueChange={(v) => setTheme(v as Theme)}>
          <DropdownMenuRadioItem value="light">
            <Sun className="size-4" /> {t('common.themeLight')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <Moon className="size-4" /> {t('common.themeDark')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <Monitor className="size-4" /> {t('common.themeSystem')}
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function LangMenu() {
  const { t, lang, setLang } = useI18n()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-9" aria-label={t('common.language')}>
          <Languages className="size-[18px]" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuLabel>{t('common.language')}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup value={lang} onValueChange={(v) => setLang(v as Lang)}>
          {LANGS.map((l) => (
            <DropdownMenuRadioItem key={l.id} value={l.id}>
              {l.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function CommandMenu({ open, setOpen }: { open: boolean; setOpen: (v: boolean) => void }) {
  const { t, setLang } = useI18n()
  const { resolved, setTheme } = useTheme()
  const { logout } = useAuth()
  const navigate = useNavigate()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen(!open)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, setOpen])
  const run = (fn: () => void) => {
    setOpen(false)
    fn()
  }
  return (
    <CommandDialog open={open} onOpenChange={setOpen} title={t('cmd.placeholder')}>
      <CommandInput placeholder={t('cmd.placeholder')} />
      <CommandList>
        <CommandEmpty>{t('cmd.empty')}</CommandEmpty>
        <CommandGroup heading={t('cmd.pages')}>
          {NAV.flatMap((g) => g.items).map((item) => (
            <CommandItem key={item.to} value={t(item.label) + ' ' + t(item.desc)} onSelect={() => run(() => navigate({ to: item.to }))}>
              <item.icon />
              <span>{t(item.label)}</span>
              <span className="ml-auto truncate pl-4 text-xs text-muted-foreground">{t(item.desc)}</span>
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading={t('cmd.actions')}>
          <CommandItem onSelect={() => run(() => setTheme(resolved === 'dark' ? 'light' : 'dark'))}>
            {resolved === 'dark' ? <Sun /> : <Moon />}
            {t('cmd.toggleTheme')}
          </CommandItem>
          {LANGS.map((l) => (
            <CommandItem key={l.id} value={'language ' + l.label} onSelect={() => run(() => setLang(l.id))}>
              <Languages />
              {l.label}
            </CommandItem>
          ))}
          <CommandItem onSelect={() => run(logout)}>
            <LogOut />
            {t('common.logout')}
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}

function SidebarBody({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useI18n()
  const { data } = useStatus()
  return (
    <div className="flex h-full flex-col gap-6 px-3 py-4">
      <Link to="/" onClick={onNavigate} className="flex items-center gap-2.5 px-2">
        <Logo className="size-8" />
        <div className="leading-tight">
          <p className="font-semibold tracking-tight">{t('app.name')}</p>
          <p className="text-xs text-muted-foreground">{t('app.tagline')}</p>
        </div>
      </Link>
      <div className="flex-1 overflow-y-auto">
        <NavLinks onNavigate={onNavigate} />
      </div>
      <div className="rounded-xl border bg-card/60 p-3 text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>Kiro-Go</span>
          <span className="font-mono">{data?.version ? 'v' + data.version : '—'}</span>
        </div>
        <a
          href="https://github.com/springmusk026/Kiro-Go"
          target="_blank"
          rel="noreferrer"
          className="mt-1 inline-block underline-offset-4 hover:text-foreground hover:underline"
        >
          GitHub
        </a>
      </div>
    </div>
  )
}

export function AppShell({ children }: { children: ReactNode }) {
  const { t } = useI18n()
  const { logout } = useAuth()
  const [cmdOpen, setCmdOpen] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const active = useActivePath()
  const current = NAV.flatMap((g) => g.items).find((i) => (i.to === '/' ? active === '/' : active.startsWith(i.to)))

  return (
    <div className="min-h-svh bg-background lg:grid lg:grid-cols-[256px_minmax(0,1fr)]">
      <aside className="sticky top-0 hidden h-svh border-r border-sidebar-border bg-sidebar lg:block">
        <SidebarBody />
      </aside>
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-72 bg-sidebar p-0">
          <SheetTitle className="sr-only">{t('app.name')}</SheetTitle>
          <SidebarBody onNavigate={() => setMobileOpen(false)} />
        </SheetContent>
      </Sheet>

      <div className="flex min-w-0 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur-md supports-[backdrop-filter]:bg-background/70 lg:px-8">
          <Button variant="ghost" size="icon" className="size-9 lg:hidden" onClick={() => setMobileOpen(true)} aria-label="Menu">
            <Menu className="size-5" />
          </Button>
          <div className="flex min-w-0 items-center gap-2 text-sm">
            <span className="hidden text-muted-foreground sm:inline">{t('app.name')}</span>
            <span className="hidden text-muted-foreground/50 sm:inline">/</span>
            <span className="truncate font-medium">{current ? t(current.label) : ''}</span>
          </div>
          <div className="ml-auto flex items-center gap-1.5">
            <button
              onClick={() => setCmdOpen(true)}
              className="hidden h-9 items-center gap-2 rounded-lg border bg-card px-3 text-sm text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground md:flex"
            >
              <Search className="size-4" />
              <span className="pr-6">{t('cmd.hint')}</span>
              <kbd className="rounded border bg-muted px-1.5 font-mono text-[10px]">⌘K</kbd>
            </button>
            <StatusPill />
            <LangMenu />
            <ThemeMenu />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" className="size-9" onClick={logout} aria-label={t('common.logout')}>
                  <LogOut className="size-[18px]" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('common.logout')}</TooltipContent>
            </Tooltip>
          </div>
        </header>
        <main key={active} className="mx-auto w-full max-w-[1320px] flex-1 px-4 py-6 animate-rise lg:px-8 lg:py-8">
          {children}
        </main>
      </div>
      <CommandMenu open={cmdOpen} setOpen={setCmdOpen} />
      <MigrationPrompt />
    </div>
  )
}
