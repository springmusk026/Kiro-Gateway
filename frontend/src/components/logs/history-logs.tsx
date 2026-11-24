import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, History, Loader2, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { PageHeader } from '@/components/app/page-header'
import { usePrivacyMode } from '@/components/accounts/lib'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useI18n } from '@/lib/i18n'
import { hk } from '@/lib/queries.history'
import { useClearLogs } from '@/lib/queries.settings'
import type { StorageInfo } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AuditTab } from './audit-tab'
import { RequestsTab } from './requests-tab'

const LIVE_KEY = 'kiro_logs_live'
const TAB_KEY = 'kiro_logs_tab'

function read(key: string, def: string) {
  try {
    return localStorage.getItem(key) ?? def
  } catch {
    return def
  }
}
function write(key: string, v: string) {
  try {
    localStorage.setItem(key, v)
  } catch {
    /* storage unavailable */
  }
}

/** Logs page backed by the PostgreSQL request history and audit log. */
export function HistoryLogs({ storage }: { storage: StorageInfo }) {
  const { t } = useI18n()
  const confirm = useConfirm()
  const qc = useQueryClient()
  const clear = useClearLogs()
  const [privacy, setPrivacy] = usePrivacyMode()
  const [live, setLiveState] = useState(() => (typeof window === 'undefined' ? true : read(LIVE_KEY, 'true') === 'true'))
  const [tab, setTabState] = useState(() => (typeof window === 'undefined' ? 'requests' : read(TAB_KEY, 'requests')))
  const [refreshing, setRefreshing] = useState(false)

  const setLive = (v: boolean) => {
    setLiveState(v)
    write(LIVE_KEY, String(v))
  }
  const setTab = (v: string) => {
    setTabState(v)
    write(TAB_KEY, v)
  }

  const refresh = async () => {
    setRefreshing(true)
    try {
      await Promise.all([
        qc.invalidateQueries({ queryKey: tab === 'audit' ? hk.audit : hk.requests }),
        qc.invalidateQueries({ queryKey: hk.usage }),
        qc.invalidateQueries({ queryKey: hk.storage }),
      ])
    } finally {
      setRefreshing(false)
    }
  }

  const onClear = async () => {
    const ok = await confirm({
      title: t('logs.clearTitle'),
      description: t('logs.h.clearBody'),
      confirmLabel: t('logs.clear'),
      destructive: true,
    })
    if (!ok) return
    try {
      await clear.mutateAsync()
      toast.success(t('logs.cleared'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('nav.logs')}
        description={t('logs.h.pageDesc')}
        actions={
          <>
            {tab === 'requests' && (
              <label
                className={cn(
                  'flex h-9 cursor-pointer items-center gap-2 rounded-lg border bg-card px-3 text-sm',
                  live && 'border-primary/30',
                )}
              >
                <span
                  className={cn('size-2 rounded-full', live ? 'animate-pulse-ring bg-primary text-primary' : 'bg-muted-foreground/40')}
                  aria-hidden
                />
                <span className={cn('font-medium', !live && 'text-muted-foreground')}>{live ? t('logs.live') : t('logs.paused')}</span>
                <Switch checked={live} onCheckedChange={setLive} aria-label={t('logs.autoRefresh')} className="ml-1" />
              </label>
            )}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon" onClick={refresh} disabled={refreshing} aria-label={t('common.refresh')}>
                  <RefreshCw className={cn(refreshing && 'animate-spin')} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('common.refresh')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => setPrivacy(!privacy)}
                  aria-label={privacy ? t('logs.showEmails') : t('logs.hideEmails')}
                  aria-pressed={!privacy}
                >
                  {privacy ? <EyeOff /> : <Eye />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{privacy ? t('logs.showEmails') : t('logs.hideEmails')}</TooltipContent>
            </Tooltip>
            <Button variant="outline" className="text-destructive hover:text-destructive" onClick={onClear} disabled={clear.isPending}>
              {clear.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
              {t('logs.clear')}
            </Button>
          </>
        }
      />

      <Tabs value={tab} onValueChange={setTab} className="gap-4">
        <TabsList>
          <TabsTrigger value="requests" className="px-3">
            <History />
            {t('logs.h.tabRequests')}
          </TabsTrigger>
          <TabsTrigger value="audit" className="px-3">
            <ShieldCheck />
            {t('logs.h.tabAudit')}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="requests" className="animate-rise">
          <RequestsTab live={live} privacy={privacy} storage={storage} />
        </TabsContent>
        <TabsContent value="audit" className="animate-rise">
          <AuditTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
