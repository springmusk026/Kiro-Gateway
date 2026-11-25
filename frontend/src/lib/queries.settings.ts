/** React Query hooks for the settings, API keys and request-log areas. */
import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { del, get, post, put } from './api'
import { qk } from './queries'
import type {
  Account,
  ApiKeyView,
  EndpointConfig,
  ModelMapping,
  PromptFilterConfig,
  ProxyConfig,
  RequestLog,
  Settings,
  SettingsPatch,
  ThinkingConfig,
} from './types'

/** Keys local to these areas (shared ones live in `qk`). */
export const sk = {
  thinking: ['thinking'] as const,
  endpoint: ['endpoint'] as const,
  proxy: ['proxy'] as const,
  modelMappings: ['model-mappings'] as const,
  promptFilter: ['prompt-filter'] as const,
  version: ['version'] as const,
  publicModels: ['public-models'] as const,
  /** id → email lookup for the logs table (raw /accounts list). */
  accountLookup: ['accounts', 'lookup'] as const,
}

type Ok = { success: true }

// ---------- Settings ----------

export function useSettings() {
  return useQuery({ queryKey: qk.settings, queryFn: () => get<Settings>('/settings') })
}

export function useThinking() {
  return useQuery({ queryKey: sk.thinking, queryFn: () => get<ThinkingConfig>('/thinking'), staleTime: 60_000 })
}

export function useEndpointConfig() {
  return useQuery({ queryKey: sk.endpoint, queryFn: () => get<EndpointConfig>('/endpoint') })
}

export function useProxyConfig() {
  return useQuery({ queryKey: sk.proxy, queryFn: () => get<ProxyConfig>('/proxy') })
}

export function useModelMappings() {
  return useQuery({
    queryKey: sk.modelMappings,
    queryFn: async () => (await get<{ mappings: ModelMapping[] | null }>('/model-mappings')).mappings ?? [],
  })
}

export function usePromptFilter() {
  return useQuery({
    queryKey: sk.promptFilter,
    queryFn: async () => {
      const c = await get<PromptFilterConfig>('/prompt-filter')
      return { ...c, rules: c.rules ?? [] }
    },
  })
}

export function useVersion() {
  return useQuery({ queryKey: sk.version, queryFn: () => get<{ version: string }>('/version'), staleTime: Infinity })
}

/** POST a config object and invalidate the given keys afterwards. */
function useConfigMutation<V>(path: string, keys: QueryKey[], body: (v: V) => unknown = (v) => v) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: V) => post<Ok>(path, body(v)),
    onSuccess: () => Promise.all(keys.map((queryKey) => qc.invalidateQueries({ queryKey }))),
  })
}

export const useSaveSettings = () => useConfigMutation<SettingsPatch>('/settings', [qk.settings, qk.status])
export const useSaveThinking = () => useConfigMutation<ThinkingConfig>('/thinking', [sk.thinking, qk.models])
export const useSaveEndpoint = () => useConfigMutation<EndpointConfig>('/endpoint', [sk.endpoint])
export const useSaveProxy = () => useConfigMutation<ProxyConfig>('/proxy', [sk.proxy])
export const useSaveModelMappings = () =>
  useConfigMutation<ModelMapping[]>('/model-mappings', [sk.modelMappings, qk.models], (mappings) => ({ mappings }))
export const useSavePromptFilter = () => useConfigMutation<PromptFilterConfig>('/prompt-filter', [sk.promptFilter])

export function useResetStats() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => post<Ok>('/stats/reset'),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.status }),
  })
}

// ---------- API keys ----------

export interface ApiKeyInput {
  name?: string
  key?: string
  enabled?: boolean
  tokenLimit?: number
  creditLimit?: number
  // Access restrictions
  allowedModels?: string[]
  rpmLimit?: number
  expiresAt?: number
  dailyCreditLimit?: number
  // Routing rules
  accountIds?: string[]
  modelMap?: Record<string, string>
  /** Replace the key with a new random one; the response carries it once. */
  regenerate?: boolean
}

export function useApiKeys() {
  return useQuery({
    queryKey: qk.apiKeys,
    queryFn: async () => (await get<{ apiKeys: ApiKeyView[] | null }>('/api-keys')).apiKeys ?? [],
  })
}

export function useCreateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: ApiKeyInput) => post<{ success: true; id: string; key: string; apiKey: ApiKeyView }>('/api-keys', v),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.apiKeys }),
  })
}

export function useUpdateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: ApiKeyInput }) =>
      put<{ success: true; apiKey: ApiKeyView; key?: string }>(`/api-keys/${encodeURIComponent(id)}`, patch),
    onMutate: async ({ id, patch }) => {
      await qc.cancelQueries({ queryKey: qk.apiKeys })
      const prev = qc.getQueryData<ApiKeyView[]>(qk.apiKeys)
      if (prev && patch.enabled !== undefined) {
        qc.setQueryData<ApiKeyView[]>(
          qk.apiKeys,
          prev.map((k) => (k.id === id ? { ...k, enabled: patch.enabled! } : k)),
        )
      }
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.apiKeys, ctx.prev)
    },
    onSettled: () => qc.invalidateQueries({ queryKey: qk.apiKeys }),
  })
}

export function useDeleteApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => del<Ok>(`/api-keys/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.apiKeys }),
  })
}

export function useResetApiKeyUsage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => post<Ok>(`/api-keys/${encodeURIComponent(id)}/reset-usage`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.apiKeys }),
  })
}

/** Public GET /v1/models (no admin header, no API key needed). */
export interface PublicModel {
  id: string
  owned_by?: string
  supports_image?: boolean
}
export function usePublicModels(enabled: boolean) {
  return useQuery({
    queryKey: sk.publicModels,
    enabled,
    queryFn: async () => {
      const res = await fetch('/v1/models', { headers: { Accept: 'application/json' } })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const j = (await res.json()) as { data?: PublicModel[] }
      return j.data ?? []
    },
  })
}

// ---------- Logs ----------

export function useLogsLive(refetchInterval: number | false) {
  return useQuery({
    queryKey: qk.logs,
    queryFn: async () => (await get<{ logs: RequestLog[] | null }>('/logs')).logs ?? [],
    refetchInterval,
  })
}

export function useClearLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => del<Ok>('/logs'),
    onSuccess: () => {
      qc.setQueryData<RequestLog[]>(qk.logs, [])
      // DELETE /logs also clears the database history when PostgreSQL is enabled.
      return Promise.all([
        qc.invalidateQueries({ queryKey: qk.logs }),
        qc.invalidateQueries({ queryKey: ['requests'] }),
        qc.invalidateQueries({ queryKey: ['usage'] }),
      ])
    },
  })
}

/** accountId → email, from GET /accounts. Failures are tolerated (falls back to the id). */
export function useAccountLookup() {
  return useQuery({
    queryKey: sk.accountLookup,
    queryFn: async () => {
      const list = await get<Account[] | null>('/accounts')
      const map: Record<string, string> = {}
      for (const a of list ?? []) map[a.id] = a.email || a.nickname || ''
      return map
    },
    staleTime: 60_000,
    retry: false,
  })
}
