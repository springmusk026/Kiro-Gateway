import { Brain, Eye, Globe, Image as ImageIcon, List, Loader2, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { CopyButton } from '@/components/app/copy-button'
import { EmptyState } from '@/components/app/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { usePublicModels, useThinking } from '@/lib/queries.settings'

export const origin = () => (typeof window === 'undefined' ? '' : window.location.origin)

const ENDPOINTS: { method: string; path: string; label: MessageKey; tag: string }[] = [
  { method: 'POST', path: '/v1/messages', label: 'keys.epMessages', tag: 'Anthropic' },
  { method: 'POST', path: '/v1/chat/completions', label: 'keys.epChat', tag: 'OpenAI' },
  { method: 'POST', path: '/v1/responses', label: 'keys.epResponses', tag: 'OpenAI' },
  { method: 'GET', path: '/v1/models', label: 'keys.epModels', tag: 'OpenAI · Anthropic' },
  { method: 'GET', path: '/v1/stats', label: 'keys.epStats', tag: 'Kiro-Go' },
]

export function EndpointsCard() {
  const { t } = useI18n()
  const [modelsOpen, setModelsOpen] = useState(false)
  const base = origin()
  return (
    <Card className="gap-0 py-0 shadow-xs">
      <div className="flex flex-wrap items-start justify-between gap-3 px-5 pt-5 sm:px-6">
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground">
            <Globe className="size-[18px]" />
          </div>
          <div>
            <h3 className="font-semibold leading-tight">{t('keys.endpoints')}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{t('keys.endpointsDesc')}</p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={() => setModelsOpen(true)}>
          <List />
          {t('keys.viewModels')}
        </Button>
      </div>
      <div className="px-5 py-5 sm:px-6">
        <div className="mb-3 flex items-center gap-2 rounded-lg border bg-muted/40 py-1 pl-3 pr-1">
          <span className="text-xs font-medium text-muted-foreground">{t('keys.baseUrl')}</span>
          <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{base}</code>
          <CopyButton value={base} />
        </div>
        <ul className="divide-y rounded-lg border">
          {ENDPOINTS.map((e) => (
            <li key={e.path} className="group flex items-center gap-3 py-1.5 pl-3 pr-1.5">
              <Badge
                variant="outline"
                className={
                  'w-12 justify-center font-mono text-[10px] ' +
                  (e.method === 'POST' ? 'border-primary/30 text-primary' : 'border-success/40 text-success')
                }
              >
                {e.method}
              </Badge>
              <div className="min-w-0 flex-1">
                <code className="block truncate font-mono text-[13px]">{e.path}</code>
                <span className="text-xs text-muted-foreground">
                  {t(e.label)} · {e.tag}
                </span>
              </div>
              <CopyButton value={base + e.path} />
            </li>
          ))}
        </ul>
        <p className="mt-3 text-xs text-muted-foreground">{t('keys.authHeaders')}</p>
      </div>
      <ModelsDialog open={modelsOpen} onOpenChange={setModelsOpen} />
    </Card>
  )
}

function ModelsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useI18n()
  const q = usePublicModels(open)
  const thinking = useThinking()
  const suffix = thinking.data?.suffix || '-thinking'
  const [search, setSearch] = useState('')
  const list = useMemo(() => {
    const s = search.trim().toLowerCase()
    return (q.data ?? []).filter((m) => !s || m.id.toLowerCase().includes(s) || (m.owned_by ?? '').toLowerCase().includes(s))
  }, [q.data, search])
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('keys.modelsTitle')}</DialogTitle>
          <DialogDescription>{t('keys.modelsDesc')}</DialogDescription>
        </DialogHeader>
        <div className="relative">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-9" placeholder={t('common.search')} value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        {q.isLoading ? (
          <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t('common.loading')}
          </div>
        ) : q.isError ? (
          <p className="py-6 text-center text-sm text-destructive">{(q.error as Error).message}</p>
        ) : list.length === 0 ? (
          <EmptyState icon={Eye} title={t('keys.modelsEmpty')} />
        ) : (
          <ScrollArea className="h-[360px] rounded-lg border">
            <ul className="divide-y">
              {list.map((m) => (
                <li key={m.id} className="flex items-center gap-2 py-1.5 pl-3 pr-1">
                  <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{m.id}</code>
                  {m.id.endsWith(suffix) && (
                    <Badge variant="secondary" className="gap-1">
                      <Brain />
                      {t('keys.thinking')}
                    </Badge>
                  )}
                  {m.supports_image && (
                    <Badge variant="secondary" className="gap-1">
                      <ImageIcon />
                      {t('keys.vision')}
                    </Badge>
                  )}
                  <CopyButton value={m.id} />
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
        <p className="text-xs text-muted-foreground tabular">{t('keys.modelsCount', { n: list.length })}</p>
      </DialogContent>
    </Dialog>
  )
}
