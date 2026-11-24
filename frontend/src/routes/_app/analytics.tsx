import { createFileRoute } from '@tanstack/react-router'
import { AnalyticsPage } from '@/components/analytics/analytics-page'
import { isRange, type AnalyticsSearch } from '@/components/analytics/lib'

const str = (v: unknown) => (typeof v === 'string' && v ? v : undefined)

export const Route = createFileRoute('/_app/analytics')({
  validateSearch: (s: Record<string, unknown>): AnalyticsSearch => ({
    range: isRange(s.range) ? s.range : undefined,
    model: str(s.model),
    account: str(s.account),
    apiKey: str(s.apiKey),
    endpoint: str(s.endpoint),
  }),
  component: AnalyticsPage,
})
