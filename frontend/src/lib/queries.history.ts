/** React Query hooks for the PostgreSQL-backed request history, usage analytics and audit log. */
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from './api'
import type {
  AuditEvent,
  LoggingSettings,
  MigrationAction,
  MigrationResult,
  MigrationStatus,
  RequestFilters,
  RequestRecord,
  RequestsPage,
  StorageInfo,
  UsageDimension,
  UsageGroup,
  UsagePoint,
  UsageSeries,
} from './types'

export const hk = {
  storage: ['storage'] as const,
  requests: ['requests'] as const,
  request: (id: string) => ['requests', 'one', id] as const,
  usage: ['usage'] as const,
  audit: ['audit'] as const,
}

export const PAGE_SIZE = 100

function qs(params: Record<string, string | number | undefined | null>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '' || v === 0) continue
    p.set(k, String(v))
  }
  const s = p.toString()
  return s ? '?' + s : ''
}

/** Strip "all"/empty values so equal filters share a query key. */
export function normalizeFilters(f: RequestFilters): RequestFilters {
  const out: RequestFilters = {}
  for (const [k, v] of Object.entries(f) as [keyof RequestFilters, string | number | undefined][]) {
    if (v === undefined || v === '' || v === 'all' || v === 0) continue
    ;(out as Record<string, string | number>)[k] = typeof v === 'string' ? v.trim() : v
  }
  return out
}

const fetchPage = async (f: RequestFilters, before?: string, limit = PAGE_SIZE, count = false) => {
  const page = await get<RequestsPage>('/requests' + qs({ limit, before, count: count ? 1 : undefined, ...f }))
  return { records: page.records ?? [], hasMore: page.hasMore, total: page.total, totalCapped: page.totalCapped }
}

/**
 * One page of the request history (keyset cursor = id of the last row of the
 * previous page). The first page also returns the matching row count; page 1
 * can poll in live mode.
 */
export function useRequestPage(filters: RequestFilters, cursor: string | undefined, pageSize: number, refetchInterval: number | false = false) {
  const f = normalizeFilters(filters)
  return useQuery({
    queryKey: [...hk.requests, 'page', f, cursor ?? '', pageSize],
    queryFn: () => fetchPage(f, cursor, pageSize, !cursor),
    placeholderData: keepPreviousData,
    refetchInterval: cursor ? false : refetchInterval,
  })
}

export function useStorageInfo(refetchInterval: number | false = false) {
  return useQuery({ queryKey: hk.storage, queryFn: () => get<StorageInfo>('/storage'), staleTime: 30_000, refetchInterval })
}

export function useRequest(id: string | null) {
  return useQuery({
    queryKey: hk.request(id ?? ''),
    enabled: !!id,
    queryFn: () => get<RequestRecord>(`/requests/${encodeURIComponent(id!)}`),
    staleTime: Infinity,
  })
}

export function useUsageSeries(hours: number, bucketSeconds: number, opts: { enabled?: boolean; refetchInterval?: number | false } = {}) {
  return useQuery({
    queryKey: [...hk.usage, 'series', hours, bucketSeconds],
    enabled: opts.enabled ?? true,
    queryFn: async (): Promise<{ points: UsagePoint[]; bucketSeconds: number }> => {
      const s = await get<UsageSeries>('/usage/series' + qs({ hours, bucket: bucketSeconds }))
      return { points: s.points ?? [], bucketSeconds: s.bucketSeconds }
    },
    refetchInterval: opts.refetchInterval ?? 30_000,
    placeholderData: keepPreviousData,
  })
}

export function useUsageBy(dimension: UsageDimension, hours: number, limit = 10, opts: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: [...hk.usage, 'by', dimension, hours, limit],
    enabled: opts.enabled ?? true,
    queryFn: async () => (await get<{ groups: UsageGroup[] | null }>('/usage/by' + qs({ dimension, hours, limit }))).groups ?? [],
    refetchInterval: 30_000,
    placeholderData: keepPreviousData,
  })
}

export function useAudit(hours = 168, limit = 200, enabled = true) {
  return useQuery({
    queryKey: [...hk.audit, hours, limit],
    enabled,
    queryFn: async () => (await get<{ events: AuditEvent[] | null }>('/audit' + qs({ hours, limit }))).events ?? [],
    refetchInterval: 30_000,
    placeholderData: keepPreviousData,
  })
}

export function useUpdateLogging() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: LoggingSettings) => post<{ success: true }>('/logging', v),
    onSuccess: (_d, v) => {
      qc.setQueryData<StorageInfo>(hk.storage, (prev) => (prev ? { ...prev, ...v, logRetentionDays: v.logRetentionDays || 30 } : prev))
      return qc.invalidateQueries({ queryKey: hk.storage })
    },
  })
}




/** Leftover config.json compared with the database. */
export function useMigrationStatus(enabled = true) {
  return useQuery({
    queryKey: [...hk.storage, 'migration'],
    queryFn: () => get<MigrationStatus>('/storage/migration'),
    enabled,
    staleTime: 30_000,
  })
}

export function useRunMigration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { action: MigrationAction; deleteAfter: boolean }) => post<MigrationResult>('/storage/migration', v),
    onSettled: () => qc.invalidateQueries(),
  })
}
