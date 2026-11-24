/** React Query hooks for the PostgreSQL-backed request history, usage analytics and audit log. */
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from './api'
import type {
  AuditEvent,
  LoggingSettings,
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

const fetchPage = async (f: RequestFilters, before?: string, limit = PAGE_SIZE) => {
  const page = await get<RequestsPage>('/requests' + qs({ limit, before, ...f }))
  return { records: page.records ?? [], hasMore: page.hasMore }
}

export function useStorageInfo(refetchInterval: number | false = false) {
  return useQuery({ queryKey: hk.storage, queryFn: () => get<StorageInfo>('/storage'), staleTime: 30_000, refetchInterval })
}

/** Keyset-paginated request history, newest first. */
export function useRequests(filters: RequestFilters, enabled = true) {
  const f = normalizeFilters(filters)
  return useInfiniteQuery({
    queryKey: [...hk.requests, 'list', f],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => fetchPage(f, pageParam),
    getNextPageParam: (last) => (last.hasMore && last.records.length ? last.records[last.records.length - 1].id : undefined),
    placeholderData: keepPreviousData,
  })
}

/** The newest page only — polled in live mode and merged on top of the paginated list. */
export function useRequestsHead(filters: RequestFilters, refetchInterval: number | false) {
  const f = normalizeFilters(filters)
  return useQuery({
    queryKey: [...hk.requests, 'head', f],
    enabled: refetchInterval !== false,
    queryFn: () => fetchPage(f, undefined, 50),
    refetchInterval,
  })
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



