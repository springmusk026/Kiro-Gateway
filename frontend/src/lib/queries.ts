import { useQuery } from '@tanstack/react-query'
import { get } from './api'
import type { ModelsCatalog, RequestLog, Status } from './types'

export const qk = {
  status: ['status'] as const,
  logs: ['logs'] as const,
  models: ['models'] as const,
  accounts: ['accounts'] as const,
  settings: ['settings'] as const,
  apiKeys: ['api-keys'] as const,
}

export function useStatus() {
  return useQuery({ queryKey: qk.status, queryFn: () => get<Status>('/status'), refetchInterval: 10_000 })
}

export function useLogs(refetchInterval: number | false = 15_000) {
  return useQuery({
    queryKey: qk.logs,
    queryFn: async () => (await get<{ logs: RequestLog[] | null }>('/logs')).logs ?? [],
    refetchInterval,
  })
}

export function useModels() {
  return useQuery({ queryKey: qk.models, queryFn: () => get<ModelsCatalog>('/models'), staleTime: 60_000 })
}
