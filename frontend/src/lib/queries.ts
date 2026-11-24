import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from './api'
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


/** Toggle one model; updates the catalog optimistically and rolls back on error. */
export function useToggleModel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      post<{ success: boolean; disabledModels: string[] }>('/models/toggle', { id, enabled }),
    onMutate: async ({ id, enabled }) => {
      await qc.cancelQueries({ queryKey: qk.models })
      const prev = qc.getQueryData<ModelsCatalog>(qk.models)
      if (prev) {
        qc.setQueryData<ModelsCatalog>(qk.models, {
          ...prev,
          models: prev.models.map((m) => (m.id === id ? { ...m, enabled } : m)),
        })
      }
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.models, ctx.prev)
    },
    onSettled: () => qc.invalidateQueries({ queryKey: qk.models }),
  })
}

export function useRefreshModels() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => get<ModelsCatalog>('/models?refresh=1'),
    onSuccess: (data) => qc.setQueryData(qk.models, data),
  })
}
