import { PageHeader } from '@/components/app/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useI18n } from '@/lib/i18n'
import { useStorageInfo } from '@/lib/queries.history'
import { HistoryLogs } from './history-logs'
import { MemoryLogs } from './memory-logs'

/** Request logs: database history when PostgreSQL is configured, else the in-memory ring buffer. */
export function LogsPage() {
  const { t } = useI18n()
  const storage = useStorageInfo()
  if (storage.isLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('nav.logs')} description={t('page.logs')} />
        <Skeleton className="h-[74px] w-full rounded-xl" />
        <Skeleton className="h-96 w-full rounded-xl" />
      </div>
    )
  }
  return storage.data?.database ? <HistoryLogs storage={storage.data} /> : <MemoryLogs />
}
