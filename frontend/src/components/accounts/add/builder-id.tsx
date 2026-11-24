import { ArrowLeft, Loader2, Play, WifiOff } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { CopyButton } from '@/components/app/copy-button'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/lib/i18n'
import { authApi, type BuilderIdStart } from '@/lib/queries.accounts'
import { errorInfo, isNetworkError, type ErrorInfo } from '../lib'
import { ErrorCallout, FlowFooter, RegionField, UrlBox, type FlowProps } from './shared'

const MAX_BACKOFF = 30

export function BuilderIdFlow({ onBack, onSuccess }: FlowProps) {
  const { t } = useI18n()
  const [region, setRegion] = useState('us-east-1')
  const [session, setSession] = useState<BuilderIdStart | null>(null)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<ErrorInfo | null>(null)
  const [netIssue, setNetIssue] = useState(false)
  const [slowDown, setSlowDown] = useState(false)
  const [nextIn, setNextIn] = useState(0)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const tick = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const alive = useRef(true)
  const done = useRef(onSuccess)
  done.current = onSuccess

  const stop = () => {
    clearTimeout(timer.current)
    clearInterval(tick.current)
  }
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      stop()
    }
  }, [])

  const schedule = (sessionId: string, seconds: number, failures: number) => {
    stop()
    const due = Date.now() + seconds * 1000
    setNextIn(seconds)
    tick.current = setInterval(() => setNextIn(Math.max(0, Math.ceil((due - Date.now()) / 1000))), 500)
    timer.current = setTimeout(() => poll(sessionId, seconds, failures), seconds * 1000)
  }

  const poll = async (sessionId: string, interval: number, failures: number) => {
    try {
      const r = await authApi.builderIdPoll(sessionId)
      if (!alive.current) return
      setNetIssue(false)
      if (r.completed) {
        stop()
        done.current([r.account], t('accounts.builderId.success'))
        return
      }
      setSlowDown(r.status === 'slow_down')
      schedule(sessionId, r.interval || interval, 0)
    } catch (e) {
      if (!alive.current) return
      // Transient (network or gateway 5xx without a server verdict): keep polling with backoff.
      const info = errorInfo(e)
      const transient = isNetworkError(e) || info.status === 502 || info.status === 503 || info.status === 504
      if (transient) {
        setNetIssue(true)
        schedule(sessionId, Math.min(MAX_BACKOFF, interval * 2 ** Math.min(failures + 1, 3)), failures + 1)
        return
      }
      stop()
      setSession(null)
      setError(info)
    }
  }

  const start = async () => {
    setStarting(true)
    setError(null)
    try {
      const s = await authApi.builderIdStart(region.trim() || 'us-east-1')
      if (!alive.current) return
      setSession(s)
      schedule(s.sessionId, s.interval || 5, 0)
    } catch (e) {
      setError(errorInfo(e))
    } finally {
      setStarting(false)
    }
  }

  const cancel = () => {
    stop()
    setSession(null)
    setNetIssue(false)
    setSlowDown(false)
  }

  if (!session) {
    return (
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{t('accounts.builderId.intro')}</p>
        <RegionField id="bid-region" value={region} onChange={setRegion} />
        <ErrorCallout error={error} />
        <FlowFooter>
          <Button variant="ghost" onClick={onBack}>
            <ArrowLeft /> {t('common.back')}
          </Button>
          <Button onClick={start} disabled={starting}>
            {starting ? <Loader2 className="animate-spin" /> : <Play />} {t('accounts.builderId.start')}
          </Button>
        </FlowFooter>
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-col items-center gap-2 rounded-xl border bg-muted/30 px-4 py-6 text-center">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{t('accounts.builderId.yourCode')}</p>
        <div className="flex items-center gap-2">
          <span className="select-all font-mono text-3xl font-semibold tracking-[0.2em] sm:text-4xl">{session.userCode}</span>
          <CopyButton value={session.userCode} />
        </div>
        <p className="text-sm text-muted-foreground">{t('accounts.builderId.enterCode')}</p>
      </div>
      <UrlBox url={session.verificationUri} label={t('accounts.builderId.verifyUrl')} />
      <div className="flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm" aria-live="polite">
        {netIssue ? (
          <WifiOff className="size-4 text-warning" />
        ) : (
          <span className="size-2 rounded-full bg-primary text-primary animate-pulse-ring" />
        )}
        <span className="flex-1">
          {netIssue ? t('accounts.builderId.netRetry') : slowDown ? t('accounts.builderId.slowDown') : t('accounts.builderId.waiting')}
        </span>
        <span className="text-xs text-muted-foreground tabular">{t('accounts.builderId.nextCheck', { s: nextIn })}</span>
      </div>
      <FlowFooter>
        <Button variant="ghost" onClick={cancel}>
          {t('common.cancel')}
        </Button>
      </FlowFooter>
    </div>
  )
}
