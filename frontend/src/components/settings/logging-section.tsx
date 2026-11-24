import { AlertTriangle, Database, FileJson, HardDrive, ScrollText, ShieldAlert } from 'lucide-react'
import { CodeBlock } from '@/components/app/code-block'
import { MigrationSettingsBlock } from '@/components/app/migration'
import { parseBackend } from '@/components/logs/history-bits'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { useI18n } from '@/lib/i18n'
import { useStorageInfo, useUpdateLogging } from '@/lib/queries.history'
import type { LoggingSettings } from '@/lib/types'
import { cn } from '@/lib/utils'
import { SectionCard, SettingRow, runSave, useDraft } from './section-card'

const BODY_SIZES = [0, 16, 64, 256, 1024]
const DEFAULT_RETENTION = 30

function Stat({ label, value, tone }: { label: string; value: number; tone?: 'warn' | 'ok' }) {
  return (
    <div className={cn('rounded-lg border px-3 py-2.5', tone === 'warn' && value > 0 && 'border-warning/40 bg-warning/[0.07]')}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn('mt-0.5 text-lg font-semibold tabular', tone === 'warn' && value > 0 && 'text-warning')}>{value.toLocaleString()}</dd>
    </div>
  )
}

/** Storage backend and recorder health; explains how to enable PostgreSQL otherwise. */
export function StorageCard() {
  const { t } = useI18n()
  const q = useStorageInfo(10_000)
  const s = q.data
  const backend = s ? parseBackend(s.backend) : null
  const rec = s?.recorder
  const unhealthy = !!rec && (rec.dropped > 0 || rec.failed > 0)

  return (
    <SectionCard icon={HardDrive} title={t('settings.storage')} description={t('settings.storageDesc')} loading={!s}>
      {s && backend && (
        <div className="space-y-5">
          <div className="flex flex-wrap items-center gap-3">
            <div
              className={cn(
                'grid size-10 place-items-center rounded-lg',
                s.database ? 'bg-success/12 text-success' : 'bg-muted text-muted-foreground',
              )}
            >
              {s.database ? <Database className="size-5" /> : <FileJson className="size-5" />}
            </div>
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 font-medium">
                {backend.kind === 'postgres' ? 'PostgreSQL' : backend.kind === 'json' ? t('settings.storageJson') : s.backend}
                {s.database ? (
                  <Badge variant="outline" className="border-success/30 bg-success/[0.08] text-success">
                    <span className="size-1.5 rounded-full bg-success text-success animate-pulse-ring" />
                    {t('settings.storageHistoryOn')}
                  </Badge>
                ) : (
                  <Badge variant="outline" className="text-muted-foreground">
                    {t('settings.storageHistoryOff')}
                  </Badge>
                )}
              </p>
              {backend.path ? (
                <p className="truncate font-mono text-xs text-muted-foreground">{backend.path}</p>
              ) : (
                <p className="text-xs text-muted-foreground">{t('settings.storagePgHint')}</p>
              )}
            </div>
          </div>

          {s.database && <MigrationSettingsBlock />}

          {s.database && rec && (
            <div className="space-y-2">
              <p className="text-sm font-medium">{t('settings.recorder')}</p>
              <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <Stat label={t('settings.recWritten')} value={rec.written} />
                <Stat label={t('settings.recQueued')} value={rec.queued} />
                <Stat label={t('settings.recDropped')} value={rec.dropped} tone="warn" />
                <Stat label={t('settings.recFailed')} value={rec.failed} tone="warn" />
              </dl>
              {unhealthy ? (
                <p className="flex items-start gap-2 text-[13px] text-warning">
                  <AlertTriangle className="mt-0.5 size-4 shrink-0" />
                  {t('settings.recUnhealthy')}
                </p>
              ) : (
                <p className="text-[13px] text-muted-foreground">{t('settings.recHealthy')}</p>
              )}
            </div>
          )}

          {!s.database && (
            <div className="space-y-3 rounded-lg border border-dashed p-4">
              <p className="text-sm font-medium">{t('settings.enablePgTitle')}</p>
              <ol className="list-decimal space-y-1.5 pl-5 text-[13px] text-muted-foreground">
                <li>{t('settings.enablePgStep1')}</li>
                <li>{t('settings.enablePgStep2')}</li>
                <li>{t('settings.enablePgStep3')}</li>
              </ol>
              <CodeBlock
                code={`DATABASE_URL="postgres://kiro:kiro@localhost:5432/kiro?sslmode=disable"\n\n# ${t('settings.enablePgMigrate')}\nkiro-go migrate --from data/config.json --to "$DATABASE_URL"`}
              />
            </div>
          )}
        </div>
      )}
    </SectionCard>
  )
}

/** Body capture and retention (POST /logging). Only shown with the database backend. */
export function LoggingCard() {
  const { t } = useI18n()
  const q = useStorageInfo()
  const save = useUpdateLogging()
  const server: LoggingSettings | undefined = q.data
    ? { logBodiesKB: q.data.logBodiesKB, logRetentionDays: q.data.logRetentionDays || DEFAULT_RETENTION }
    : undefined
  const { draft, setDraft, dirty, reset, commit } = useDraft(server)
  if (q.data && !q.data.database) return null

  const sizes = draft && !BODY_SIZES.includes(draft.logBodiesKB) ? [...BODY_SIZES, draft.logBodiesKB].sort((a, b) => a - b) : BODY_SIZES
  const retentionInvalid = !!draft && (!Number.isInteger(draft.logRetentionDays) || draft.logRetentionDays < 1 || draft.logRetentionDays > 3650)

  return (
    <SectionCard
      icon={ScrollText}
      title={t('settings.logging')}
      description={t('settings.loggingDesc')}
      loading={!draft}
      dirty={dirty}
      saving={save.isPending}
      saveDisabled={retentionInvalid}
      onReset={reset}
      onSave={async () => {
        if (draft && (await runSave(() => save.mutateAsync(draft), t('settings.loggingSaved')))) commit()
      }}
    >
      {draft && (
        <div className="space-y-6">
          <div className="space-y-3">
            <div>
              <p className="text-sm font-medium" id="body-capture-label">
                {t('settings.bodyCapture')}
              </p>
              <p className="mt-0.5 text-[13px] text-muted-foreground">{t('settings.bodyCaptureHint')}</p>
            </div>
            <div role="radiogroup" aria-labelledby="body-capture-label" className="grid grid-cols-3 gap-2 sm:grid-cols-5">
              {sizes.map((kb) => {
                const active = draft.logBodiesKB === kb
                return (
                  <button
                    key={kb}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setDraft((d) => ({ ...d, logBodiesKB: kb }))}
                    className={cn(
                      'h-10 rounded-lg border text-sm font-medium tabular transition-colors hover:border-primary/40',
                      active && 'border-primary bg-primary/[0.05] text-primary ring-1 ring-primary/30',
                    )}
                  >
                    {kb === 0 ? t('settings.bodyOff') : kb >= 1024 ? `${kb / 1024} MB` : `${kb} KB`}
                  </button>
                )
              })}
            </div>
            {draft.logBodiesKB > 0 && (
              <Alert className="border-warning/40 bg-warning/10 animate-rise">
                <ShieldAlert className="text-warning" />
                <AlertTitle>{t('settings.bodyPrivacyTitle')}</AlertTitle>
                <AlertDescription>{t('settings.bodyPrivacyBody', { kb: draft.logBodiesKB })}</AlertDescription>
              </Alert>
            )}
          </div>

          <SettingRow
            id="log-retention"
            label={t('settings.retention')}
            hint={retentionInvalid ? <span className="text-destructive">{t('settings.retentionInvalid')}</span> : t('settings.retentionHint')}
          >
            <div className="flex items-center gap-2">
              <Input
                id="log-retention"
                type="number"
                min={1}
                max={3650}
                className="w-24 text-right tabular"
                aria-invalid={retentionInvalid}
                value={Number.isNaN(draft.logRetentionDays) ? '' : draft.logRetentionDays}
                onChange={(e) => setDraft((d) => ({ ...d, logRetentionDays: e.target.value === '' ? NaN : Number(e.target.value) }))}
              />
              <span className="text-sm text-muted-foreground">{t('settings.days')}</span>
            </div>
          </SettingRow>
        </div>
      )}
    </SectionCard>
  )
}
