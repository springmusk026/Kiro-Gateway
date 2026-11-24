import { BarChart3, Info, Loader2, RotateCcw, Terminal } from 'lucide-react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { compact, duration } from '@/lib/format'
import { useStatus } from '@/lib/queries'
import { useResetStats, useSettings, useVersion } from '@/lib/queries.settings'
import { SectionCard } from './section-card'

export function ResetStatsCard() {
  const { t } = useI18n()
  const confirm = useConfirm()
  const status = useStatus()
  const reset = useResetStats()
  const s = status.data
  return (
    <SectionCard icon={BarChart3} title={t('settings.stats')} description={t('settings.statsDesc')}>
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <dl className="grid grid-cols-3 gap-6 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.statRequests')}</dt>
            <dd className="mt-0.5 font-semibold tabular">{s ? compact(s.totalRequests) : '–'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.statTokens')}</dt>
            <dd className="mt-0.5 font-semibold tabular">{s ? compact(s.totalTokens) : '–'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.statCredits')}</dt>
            <dd className="mt-0.5 font-semibold tabular">{s ? s.totalCredits.toFixed(2) : '–'}</dd>
          </div>
        </dl>
        <Button
          variant="outline"
          className="text-destructive hover:text-destructive"
          disabled={reset.isPending}
          onClick={async () => {
            const ok = await confirm({
              title: t('settings.resetStatsTitle'),
              description: t('settings.resetStatsBody'),
              confirmLabel: t('settings.resetStats'),
              destructive: true,
            })
            if (!ok) return
            try {
              await reset.mutateAsync()
              toast.success(t('settings.statsReset'))
            } catch (e) {
              toast.error(e instanceof Error ? e.message : String(e))
            }
          }}
        >
          {reset.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
          {t('settings.resetStats')}
        </Button>
      </div>
    </SectionCard>
  )
}

const ENV: { name: string; desc: MessageKey; def: string }[] = [
  { name: 'CONFIG_PATH', desc: 'settings.env.CONFIG_PATH', def: 'data/config.json' },
  { name: 'ADMIN_PASSWORD', desc: 'settings.env.ADMIN_PASSWORD', def: '—' },
  { name: 'LOG_LEVEL', desc: 'settings.env.LOG_LEVEL', def: 'info' },
  { name: 'KIRO_UPSTREAM_PROTOCOL', desc: 'settings.env.KIRO_UPSTREAM_PROTOCOL', def: 'auto' },
  { name: 'KIRO_SESSION_AFFINITY', desc: 'settings.env.KIRO_SESSION_AFFINITY', def: 'off' },
  { name: 'KIRO_MAX_UPSTREAM_ATTEMPTS', desc: 'settings.env.KIRO_MAX_UPSTREAM_ATTEMPTS', def: '4' },
  { name: 'KIRO_MAX_CONCURRENT_PER_ACCOUNT', desc: 'settings.env.KIRO_MAX_CONCURRENT_PER_ACCOUNT', def: '0' },
  { name: 'KIRO_STREAM_IDLE_TIMEOUT', desc: 'settings.env.KIRO_STREAM_IDLE_TIMEOUT', def: '180' },
  { name: 'KIRO_MAX_BODY_MB', desc: 'settings.env.KIRO_MAX_BODY_MB', def: '64' },
  { name: 'KIRO_MAX_STORED_RESPONSES', desc: 'settings.env.KIRO_MAX_STORED_RESPONSES', def: '20000' },
  { name: 'KIRO_MAX_STORED_RESPONSES_MB', desc: 'settings.env.KIRO_MAX_STORED_RESPONSES_MB', def: '2048' },
  { name: 'KIRO_KEEP_STRUCTURED_TOOL_HISTORY', desc: 'settings.env.KIRO_KEEP_STRUCTURED_TOOL_HISTORY', def: 'off' },
  { name: 'KIRO_GO_RTK', desc: 'settings.env.KIRO_GO_RTK', def: 'on' },
]

export function AboutCard() {
  const { t } = useI18n()
  const version = useVersion()
  const status = useStatus()
  const settings = useSettings()
  return (
    <SectionCard icon={Info} title={t('settings.about')} description={t('settings.aboutDesc')}>
      <div className="space-y-5">
        <dl className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.version')}</dt>
            <dd className="mt-0.5 font-mono">{version.data?.version ? 'v' + version.data.version.replace(/^v/, '') : '–'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.listen')}</dt>
            <dd className="mt-0.5 font-mono">{settings.data ? `${settings.data.host || '0.0.0.0'}:${settings.data.port}` : '–'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.uptime')}</dt>
            <dd className="mt-0.5 tabular">{status.data ? duration(status.data.uptime) : '–'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t('settings.accountsLabel')}</dt>
            <dd className="mt-0.5 tabular">
              {status.data ? `${status.data.available} / ${status.data.accounts}` : '–'}
            </dd>
          </div>
        </dl>
        <div>
          <p className="flex items-center gap-2 text-sm font-medium">
            <Terminal className="size-4 text-muted-foreground" />
            {t('settings.envTitle')}
          </p>
          <p className="mt-0.5 text-[13px] text-muted-foreground">{t('settings.envHint')}</p>
          <div className="mt-3 divide-y rounded-lg border">
            {ENV.map((e) => (
              <div key={e.name} className="flex flex-col gap-1 px-3 py-2.5 sm:flex-row sm:items-center sm:gap-4">
                <code className="shrink-0 font-mono text-[12.5px] sm:w-72">{e.name}</code>
                <span className="min-w-0 flex-1 text-[13px] text-muted-foreground">{t(e.desc)}</span>
                <Badge variant="outline" className="shrink-0 font-mono text-[11px]">
                  {e.def}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      </div>
    </SectionCard>
  )
}
