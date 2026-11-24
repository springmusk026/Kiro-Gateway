/** React Query hook for the PostgreSQL-backed analytics report (GET /analytics). */
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ApiError, get } from './api'
import type { AnalyticsFilters, AnalyticsReport } from './types'

export const ak = {
  analytics: ['analytics'] as const,
}

const localTz = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

function clean(f: AnalyticsFilters): AnalyticsFilters {
  const out: AnalyticsFilters = {}
  for (const k of ['model', 'account', 'apiKey', 'endpoint'] as const) if (f[k]) out[k] = f[k]
  return out
}

/** One full report for a window; refreshed every minute, keeping the previous data while refetching. */
export function useAnalytics(hours: number, filters: AnalyticsFilters, enabled = true) {
  const f = clean(filters)
  const tz = localTz()
  return useQuery({
    queryKey: [...ak.analytics, hours, tz, f],
    enabled,
    queryFn: () => {
      const p = new URLSearchParams({ hours: String(hours), tz })
      for (const [k, v] of Object.entries(f)) p.set(k, v as string)
      return get<AnalyticsReport>('/analytics?' + p.toString())
    },
    placeholderData: keepPreviousData,
    refetchInterval: 60_000,
    staleTime: 30_000,
    // 409 = no database: retrying will not help.
    retry: (n, err) => !(err instanceof ApiError && err.status === 409) && n < 2,
  })
}
