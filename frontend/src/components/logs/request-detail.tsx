import { Link, useNavigate } from '@tanstack/react-router'
import { FileX2, MessagesSquare, Sparkles } from 'lucide-react'
import { toast } from 'sonner'
import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { CopyButton } from '@/components/app/copy-button'
import { prefillFromBody, stashPrefill } from '@/components/playground/prefill'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { timeAgo, usd } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useRequest } from '@/lib/queries.history'
import type { RequestRecord } from '@/lib/types'
import { cn } from '@/lib/utils'
import { exactTime, fmtBytes, fmtMs, prettyBody, unix } from './history-bits'
import { ERROR_TYPES, EndpointChip, ErrorTypeBadge, StatusBadge } from './log-meta'
import type { useRecordLabels } from './requests-tab'

type Labels = ReturnType<typeof useRecordLabels>

const LONG_VALUE = 160

/** Long single values (paths, user agents, IDs) collapse to a few lines. */
function LongValue({ value }: { value: string }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  return (
    <span className="block">
      <span className={cn('block', !open && 'line-clamp-3')} title={open ? undefined : value}>
        {value}
      </span>
      <span className="mt-0.5 flex items-center gap-1 font-sans">
        <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => setOpen((o) => !o)}>
          {open ? t('logs.h.showLess') : t('logs.h.showMore', { n: value.length.toLocaleString() })}
        </button>
        <CopyButton value={value} className="size-6" />
      </span>
    </span>
  )
}

function Group({ title, rows }: { title: string; rows: [string, ReactNode][] }) {
  return (
    <section className="min-w-0 rounded-xl border bg-card p-4">
      <h3 className="mb-2.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h3>
      <dl className="grid grid-cols-[minmax(96px,auto)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
        {rows.map(([k, v]) => (
          <Fragment key={k}>
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="min-w-0 break-all [overflow-wrap:anywhere] font-mono text-[12.5px]">
              {typeof v === 'string' && v.length > LONG_VALUE ? <LongValue value={v} /> : v}
            </dd>
          </Fragment>
        ))}
      </dl>
    </section>
  )
}

function Body({ text }: { text: string }) {
  const { t } = useI18n()
  const pretty = useMemo(() => prettyBody(text), [text])
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span>
          {pretty.json ? 'JSON' : t('logs.h.rawText')} · {fmtBytes(new TextEncoder().encode(text).length)}
        </span>
        <CopyButton value={pretty.text} label={t('common.copy')} />
      </div>
      <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere] rounded-lg border bg-muted/40 p-3 font-mono text-[12px] leading-relaxed">
        {pretty.text}
      </pre>
    </div>
  )
}

export function RequestDetailSheet({
  record,
  onClose,
  labels,
  bodiesEnabled,
  currentConversation,
}: {
  record: RequestRecord | null
  onClose: () => void
  labels: Labels
  bodiesEnabled: boolean
  /** The conversation already on screen: no link back to it. */
  currentConversation?: string
}) {
  const { t, lang } = useI18n()
  const navigate = useNavigate()
  const full = useRequest(record?.id ?? null)
  const r = full.data ?? record
  const meta = r?.errorType ? ERROR_TYPES.find((e) => e.id === r.errorType) : undefined
  const hasBodies = !!(full.data?.requestBody || full.data?.responseBody)
  const prefill = useMemo(
    () => (full.data ? prefillFromBody(full.data.requestBody, full.data.endpoint, full.data.model ?? '', full.data.id) : null),
    [full.data],
  )
  const openInPlayground = () => {
    if (!prefill) return
    if (!stashPrefill({ ...prefill, savedAt: Date.now() })) {
      toast.error(t('logs.pg.storageError'))
      return
    }
    onClose()
    navigate({ to: '/playground', search: { prefill: 1 } })
  }
  const showConversation = !!r?.conversationId && r.conversationId !== currentConversation

  return (
    <Sheet open={!!record} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 p-0 sm:max-w-2xl">
        {r && (
          <>
            <SheetHeader className="border-b p-5 pr-12">
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge log={r} />
                <EndpointChip endpoint={r.endpoint} />
                {r.httpStatus > 0 && (
                  <Badge variant="outline" className={cn('font-mono', r.httpStatus >= 400 && 'border-destructive/30 text-destructive')}>
                    HTTP {r.httpStatus}
                  </Badge>
                )}
              </div>
              <SheetTitle className="truncate pt-1 font-mono text-base">{r.model || r.path}</SheetTitle>
              <SheetDescription className="flex flex-wrap items-center gap-x-1.5 text-xs">
                <span>{exactTime(r.time, lang)}</span>
                <span aria-hidden>·</span>
                <span>{timeAgo(unix(r.time), lang)}</span>
              </SheetDescription>
              <div className="mt-2 flex items-center gap-1 rounded-lg border bg-muted/40 py-1 pl-3 pr-1">
                <span className="shrink-0 text-xs text-muted-foreground">{t('logs.h.requestId')}</span>
                <code className="min-w-0 flex-1 truncate font-mono text-[12.5px]">{r.id}</code>
                <CopyButton value={r.id} />
              </div>
              {(showConversation || prefill) && (
                <div className="mt-2 flex flex-wrap gap-2">
                  {showConversation && (
                    <Button asChild variant="outline" size="sm">
                      <Link to="/logs" search={{ conversation: r.conversationId }} onClick={onClose}>
                        <MessagesSquare />
                        {t('logs.c.view')}
                      </Link>
                    </Button>
                  )}
                  {prefill && (
                    <Button variant="outline" size="sm" onClick={openInPlayground}>
                      <Sparkles />
                      {t('logs.pg.open')}
                    </Button>
                  )}
                </div>
              )}
            </SheetHeader>
            <ScrollArea className="min-h-0 flex-1">
              <div className="space-y-4 p-5 stagger">
                {r.status !== 'success' && (
                  <section className="space-y-2 rounded-xl border border-destructive/30 bg-destructive/[0.04] p-4">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-2">
                        {r.errorType && <ErrorTypeBadge type={r.errorType} />}
                        {meta && <span className="truncate text-xs text-muted-foreground">{t(meta.hint)}</span>}
                      </div>
                      {r.error && <CopyButton value={r.error} label={t('common.copy')} />}
                    </div>
                    <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere] font-mono text-[12px] leading-relaxed text-destructive">
                      {r.error || t('logs.noErrorText')}
                    </pre>
                  </section>
                )}

                <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
                  <Group
                    title={t('logs.h.grpRequest')}
                    rows={[
                      [t('logs.h.method'), `${r.method} ${r.path}`],
                      [t('logs.colModel'), r.model || '–'],
                      ...(r.fallbackModel ? [[t('cost.fbLogLabel'), r.fallbackModel] as [string, string]] : []),
                      [t('logs.h.stream'), r.stream ? t('logs.h.yes') : t('logs.h.no')],
                      [t('logs.h.colProtocol'), r.protocol || '–'],
                      ...(r.conversationId ? [[t('logs.c.id'), r.conversationId] as [string, string]] : []),
                    ]}
                  />
                  <Group
                    title={t('logs.h.grpRouting')}
                    rows={[
                      [t('logs.colAccount'), labels.account(r)],
                      [t('logs.accountId'), r.accountId || '–'],
                      [t('logs.h.colKey'), labels.apiKey(r.apiKeyId)],
                      [t('logs.h.colAttempts'), String(r.attempts || '–')],
                    ]}
                  />
                  <Group
                    title={t('logs.h.grpPerf')}
                    rows={[
                      [t('logs.colDuration'), r.durationMs ? fmtMs(r.durationMs) : '–'],
                      [t('logs.h.ttfb'), r.ttfbMs ? fmtMs(r.ttfbMs) : '–'],
                      [t('logs.h.reqBytes'), fmtBytes(r.requestBytes)],
                      [t('logs.h.respBytes'), fmtBytes(r.responseBytes)],
                    ]}
                  />
                  <Group
                    title={t('logs.h.grpUsage')}
                    rows={[
                      [t('logs.h.inputTokens'), r.inputTokens.toLocaleString(lang)],
                      [t('logs.h.outputTokens'), r.outputTokens.toLocaleString(lang)],
                      [t('logs.colCredits'), r.credits ? String(r.credits) : '–'],
                      [t('pricing.listPrice'), r.costUsd ? usd(r.costUsd) : '–'],
                    ]}
                  />
                </div>
                <Group
                  title={t('logs.h.grpClient')}
                  rows={[
                    [t('logs.h.ip'), r.clientIp || '–'],
                    [t('logs.h.userAgent'), r.userAgent || '–'],
                  ]}
                />

                <section className="min-w-0 rounded-xl border bg-card p-4">
                  <h3 className="mb-2.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('logs.h.bodies')}</h3>
                  {full.isLoading ? (
                    <Skeleton className="h-32 w-full" />
                  ) : hasBodies ? (
                    <Tabs defaultValue={full.data?.requestBody ? 'req' : 'resp'}>
                      <TabsList>
                        <TabsTrigger value="req" disabled={!full.data?.requestBody}>
                          {t('logs.h.reqBody')}
                        </TabsTrigger>
                        <TabsTrigger value="resp" disabled={!full.data?.responseBody}>
                          {t('logs.h.respBody')}
                        </TabsTrigger>
                      </TabsList>
                      <TabsContent value="req" className="pt-2">
                        {full.data?.requestBody && <Body text={full.data.requestBody} />}
                      </TabsContent>
                      <TabsContent value="resp" className="pt-2">
                        {full.data?.responseBody && <Body text={full.data.responseBody} />}
                      </TabsContent>
                    </Tabs>
                  ) : (
                    <div className="flex items-start gap-3 rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                      <FileX2 className="mt-0.5 size-4 shrink-0" />
                      <p>
                        {bodiesEnabled ? t('logs.h.noBodies') : t('logs.h.bodiesOff')}{' '}
                        {!bodiesEnabled && (
                          <Link to="/settings" hash="logging" className="font-medium text-primary hover:underline">
                            {t('logs.h.bodiesOffAction')}
                          </Link>
                        )}
                      </p>
                    </div>
                  )}
                </section>
              </div>
            </ScrollArea>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
