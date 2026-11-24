import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { ArrowRight, Boxes, Eye, FlaskConical, Info, Loader2, RefreshCw, Search, Type } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { CopyButton } from '@/components/app/copy-button'
import { EmptyState } from '@/components/app/empty-state'
import { PageHeader } from '@/components/app/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { compact, timeAgo } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { useModels, useRefreshModels, useToggleModel } from '@/lib/queries'
import type { CatalogModel } from '@/lib/types'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/_app/models')({ component: ModelsPage })

type Filter = 'all' | 'enabled' | 'disabled'

function ModelsPage() {
  const { t, lang } = useI18n()
  const { data, isLoading, error, refetch } = useModels()
  const refresh = useRefreshModels()
  const toggle = useToggleModel()
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [bulkBusy, setBulkBusy] = useState(false)

  const models = data?.models ?? []
  const enabledCount = models.filter((m) => m.enabled).length
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return models.filter(
      (m) =>
        (filter === 'all' || (filter === 'enabled' ? m.enabled : !m.enabled)) &&
        (!needle || `${m.id} ${m.name ?? ''} ${m.description ?? ''}`.toLowerCase().includes(needle)),
    )
  }, [models, q, filter])

  const onToggle = (m: CatalogModel, enabled: boolean) =>
    toggle.mutate(
      { id: m.id, enabled },
      {
        onSuccess: () => toast.success(t(enabled ? 'models.enabledToast' : 'models.disabledToast', { id: m.id })),
        onError: (e) => toast.error(t('models.toggleFailed', { id: m.id }), { description: e.message }),
      },
    )

  const bulk = async (enabled: boolean) => {
    const targets = shown.filter((m) => m.enabled !== enabled)
    if (!targets.length) return
    setBulkBusy(true)
    try {
      for (const m of targets) await toggle.mutateAsync({ id: m.id, enabled })
      toast.success(t('models.bulkDone', { n: targets.length }))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBulkBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('nav.models')}
        description={t('page.models')}
        actions={
          <Button
            onClick={() =>
              refresh.mutate(undefined, {
                onSuccess: () => toast.success(t('models.refreshed')),
                onError: (e) => toast.error(e.message),
              })
            }
            disabled={refresh.isPending}
          >
            <RefreshCw className={cn('size-4', refresh.isPending && 'animate-spin')} />
            {t('models.refresh')}
          </Button>
        }
      />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
        <Card className="gap-0 overflow-hidden py-0">
          <div className="flex flex-col gap-3 border-b p-4 md:flex-row md:items-center">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('models.search')} className="pl-9" />
            </div>
            <ToggleGroup type="single" variant="outline" value={filter} onValueChange={(v) => v && setFilter(v as Filter)}>
              <ToggleGroupItem value="all" className="px-3">{t('models.filterAll')}</ToggleGroupItem>
              <ToggleGroupItem value="enabled" className="px-3">{t('models.filterEnabled')}</ToggleGroupItem>
              <ToggleGroupItem value="disabled" className="px-3">{t('models.filterDisabled')}</ToggleGroupItem>
            </ToggleGroup>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={bulkBusy || !shown.some((m) => !m.enabled)} onClick={() => bulk(true)}>
                {t('models.enableAll')}
              </Button>
              <Button variant="outline" size="sm" disabled={bulkBusy || !shown.some((m) => m.enabled)} onClick={() => bulk(false)}>
                {bulkBusy && <Loader2 className="size-3.5 animate-spin" />}
                {t('models.disableAll')}
              </Button>
            </div>
          </div>

          {isLoading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-14 w-full" />
              ))}
            </div>
          ) : error ? (
            <EmptyState
              icon={Boxes}
              title={t('common.error')}
              description={(error as Error).message}
              action={<Button variant="outline" onClick={() => refetch()}>{t('common.retry')}</Button>}
            />
          ) : !models.length ? (
            <EmptyState icon={Boxes} title={t('models.empty')} description={t('models.emptyHint')} />
          ) : !shown.length ? (
            <EmptyState icon={Search} title={t('models.noMatch')} />
          ) : (
            <ModelTable models={shown} onToggle={onToggle} suffix={data?.thinkingSuffix || '-thinking'} />
          )}
        </Card>

        <div className="space-y-4">
          <Card className="gap-3">
            <CardHeader>
              <CardTitle className="text-base">{t('models.summary', { enabled: enabledCount, total: models.length })}</CardTitle>
              <CardDescription>
                {data?.cachedAt
                  ? t('models.updated', { ago: timeAgo(data.cachedAt, lang), min: Math.round((data.ttlSeconds || 1800) / 60) })
                  : t('models.never')}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-primary transition-[width] duration-500"
                  style={{ width: models.length ? `${(100 * enabledCount) / models.length}%` : 0 }}
                />
              </div>
              <p className="mt-3 flex gap-2 text-xs leading-relaxed text-muted-foreground">
                <Info className="mt-0.5 size-3.5 shrink-0" />
                {t('models.disabledHelp')}
              </p>
            </CardContent>
          </Card>
          <Card className="gap-3">
            <CardHeader>
              <CardTitle className="text-base">{t('models.aliases')}</CardTitle>
              <CardDescription>{t('models.aliasesHint')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              {data?.mappings?.length ? (
                data.mappings.map((m) => (
                  <div key={m.source} className="flex items-center gap-2 rounded-lg border bg-muted/40 px-2.5 py-1.5 font-mono text-xs">
                    <span className="truncate">{m.source}</span>
                    <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
                    <span className="truncate text-muted-foreground">{m.target}</span>
                  </div>
                ))
              ) : (
                <p className="text-sm text-muted-foreground">{t('models.noAliases')}</p>
              )}
              <Button asChild variant="link" className="h-auto px-0">
                <Link to="/settings">{t('models.manageAliases')} →</Link>
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

function ModelTable({ models, onToggle, suffix }: { models: CatalogModel[]; onToggle: (m: CatalogModel, v: boolean) => void; suffix: string }) {
  const { t } = useI18n()
  const navigate = useNavigate()
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
            <th className="px-4 py-2.5 font-medium">{t('models.colModel')}</th>
            <th className="px-4 py-2.5 text-right font-medium">{t('models.colContext')}</th>
            <th className="hidden px-4 py-2.5 text-right font-medium md:table-cell">{t('models.colOutput')}</th>
            <th className="hidden px-4 py-2.5 font-medium md:table-cell">{t('models.colInputs')}</th>
            <th className="px-4 py-2.5 text-right font-medium">{t('models.colRate')}</th>
            <th className="hidden px-4 py-2.5 text-right font-medium sm:table-cell">{t('models.colAccounts')}</th>
            <th className="px-4 py-2.5 text-right font-medium">{t('models.colEnabled')}</th>
          </tr>
        </thead>
        <tbody className="stagger">
          {models.map((m) => (
            <tr key={m.id} className={cn('group border-b transition-colors last:border-0 hover:bg-muted/40', !m.enabled && 'bg-muted/20')}>
              <td className="px-4 py-3">
                <div className={cn('flex items-center gap-2 transition-opacity', !m.enabled && 'opacity-55')}>
                  <div className="min-w-0">
                    <div className="flex items-center gap-1">
                      <span className="truncate font-mono text-[13px] font-medium">{m.id}</span>
                      <span className="flex opacity-0 transition-opacity group-hover:opacity-100">
                        <CopyButton value={m.id} className="size-7" />
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-7"
                              aria-label={t('models.try')}
                              disabled={!m.enabled}
                              onClick={() => navigate({ to: '/playground', search: { model: m.id } })}
                            >
                              <FlaskConical className="size-3.5" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>{t('models.try')}</TooltipContent>
                        </Tooltip>
                      </span>
                    </div>
                    <p className="truncate text-xs text-muted-foreground">
                      {m.name && m.name !== m.id ? m.name : m.description || t('models.thinking', { suffix })}
                    </p>
                  </div>
                </div>
              </td>
              <td className="px-4 py-3 text-right tabular">{compact(m.contextLength)}</td>
              <td className="hidden px-4 py-3 text-right tabular md:table-cell">{compact(m.maxOutputTokens)}</td>
              <td className="hidden px-4 py-3 md:table-cell">
                <div className="flex gap-1">
                  <Badge variant="secondary" className="gap-1 font-normal">
                    <Type className="size-3" />
                    {t('models.text')}
                  </Badge>
                  {m.vision && (
                    <Badge variant="secondary" className="gap-1 bg-accent font-normal text-accent-foreground">
                      <Eye className="size-3" />
                      {t('models.vision')}
                    </Badge>
                  )}
                </div>
              </td>
              <td className="px-4 py-3 text-right tabular">{m.rateMultiplier ? `${m.rateMultiplier}×` : '–'}</td>
              <td className="hidden px-4 py-3 text-right sm:table-cell">
                {m.accounts ? (
                  <span className="tabular">{m.accounts}</span>
                ) : (
                  <Tooltip>
                    <TooltipTrigger className="text-muted-foreground">0</TooltipTrigger>
                    <TooltipContent>{t('models.noAccounts')}</TooltipContent>
                  </Tooltip>
                )}
              </td>
              <td className="px-4 py-3 text-right">
                <Switch checked={m.enabled} onCheckedChange={(v) => onToggle(m, v)} aria-label={`${m.id} ${t('models.colEnabled')}`} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
