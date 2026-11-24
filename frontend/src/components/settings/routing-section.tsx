import { Gauge, Route } from 'lucide-react'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useI18n } from '@/lib/i18n'
import type { MessageKey } from '@/locales/en'
import { useEndpointConfig, useSaveEndpoint, useSaveSettings, useSettings } from '@/lib/queries.settings'
import type { EndpointConfig } from '@/lib/types'
import { cn } from '@/lib/utils'
import { SectionCard, SettingRow, runSave, useDraft } from './section-card'

const PROTOCOLS: { id: EndpointConfig['upstreamProtocol']; label: MessageKey; hint: MessageKey }[] = [
  { id: 'auto', label: 'settings.protoAuto', hint: 'settings.protoAutoHint' },
  { id: 'responses', label: 'settings.protoResponses', hint: 'settings.protoResponsesHint' },
  { id: 'legacy', label: 'settings.protoLegacy', hint: 'settings.protoLegacyHint' },
]
const ENDPOINTS: { id: EndpointConfig['preferredEndpoint']; label: MessageKey }[] = [
  { id: 'auto', label: 'settings.epAuto' },
  { id: 'kiro', label: 'settings.epKiro' },
  { id: 'codewhisperer', label: 'settings.epCodeWhisperer' },
  { id: 'amazonq', label: 'settings.epAmazonQ' },
]

export function EndpointCard() {
  const { t } = useI18n()
  const q = useEndpointConfig()
  const save = useSaveEndpoint()
  const { draft, setDraft, dirty, reset, commit } = useDraft(q.data)

  return (
    <SectionCard
      icon={Route}
      title={t('settings.upstream')}
      description={t('settings.upstreamDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync(draft), t('settings.saved')))) commit()
      }}
    >
      {draft && (
        <div className="space-y-6">
          <div className="space-y-2">
            <Label>{t('settings.protocol')}</Label>
            <div role="radiogroup" className="grid gap-2 sm:grid-cols-3">
              {PROTOCOLS.map((p) => {
                const active = (draft.upstreamProtocol || 'auto') === p.id
                return (
                  <button
                    key={p.id}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setDraft((d) => ({ ...d, upstreamProtocol: p.id }))}
                    className={cn(
                      'rounded-lg border p-3 text-left transition-colors hover:border-primary/40',
                      active && 'border-primary bg-primary/[0.05] ring-1 ring-primary/30',
                    )}
                  >
                    <span className="flex items-center gap-2 text-sm font-medium">
                      <span
                        className={cn(
                          'grid size-3.5 place-items-center rounded-full border',
                          active && 'border-primary',
                        )}
                      >
                        {active && <span className="size-1.5 rounded-full bg-primary" />}
                      </span>
                      {t(p.label)}
                    </span>
                    <span className="mt-1 block text-xs leading-relaxed text-muted-foreground">{t(p.hint)}</span>
                  </button>
                )
              })}
            </div>
            <p className="text-xs text-muted-foreground">{t('settings.protocolEnv')}</p>
          </div>
          <SettingRow id="pref-ep" label={t('settings.preferredEndpoint')} hint={t('settings.preferredEndpointHint')}>
            <Select
              value={draft.preferredEndpoint || 'auto'}
              onValueChange={(v) => setDraft((d) => ({ ...d, preferredEndpoint: v as EndpointConfig['preferredEndpoint'] }))}
            >
              <SelectTrigger id="pref-ep" className="w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ENDPOINTS.map((e) => (
                  <SelectItem key={e.id} value={e.id}>
                    {t(e.label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SettingRow>
          <SettingRow
            id="ep-fallback"
            label={t('settings.fallback')}
            hint={draft.preferredEndpoint === 'auto' ? t('settings.fallbackAutoHint') : t('settings.fallbackHint')}
          >
            <Switch
              id="ep-fallback"
              checked={draft.endpointFallback}
              disabled={draft.preferredEndpoint === 'auto'}
              onCheckedChange={(v) => setDraft((d) => ({ ...d, endpointFallback: v }))}
            />
          </SettingRow>
          <p className="text-xs text-muted-foreground">{t('settings.apiKeyAccountsNote')}</p>
        </div>
      )}
    </SectionCard>
  )
}

export function UsageCard() {
  const { t } = useI18n()
  const settings = useSettings()
  const save = useSaveSettings()
  const server = settings.data ? { allowOverUsage: settings.data.allowOverUsage } : undefined
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  return (
    <SectionCard
      icon={Gauge}
      title={t('settings.usage')}
      description={t('settings.usageDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync({ allowOverUsage: draft.allowOverUsage }), t('settings.saved'))))
          commit()
      }}
    >
      {draft && (
        <SettingRow id="over-usage" label={t('settings.allowOverUsage')} hint={t('settings.allowOverUsageHint')}>
          <Switch
            id="over-usage"
            checked={draft.allowOverUsage}
            onCheckedChange={(v) => setDraft((d) => ({ ...d, allowOverUsage: v }))}
          />
        </SettingRow>
      )}
    </SectionCard>
  )
}
