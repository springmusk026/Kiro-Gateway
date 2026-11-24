/** React Query hooks for the accounts area. */
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api, del, get, post, put } from './api'
import { qk } from './queries'
import type {
  Account,
  AccountFull,
  AccountPatch,
  CredentialImport,
  KiroProfile,
  NewAccountRef,
  OverageResponse,
  UpstreamModelInfo,
} from './types'

export const accountKeys = {
  all: qk.accounts,
  models: (id: string) => ['accounts', id, 'models'] as const,
  cachedModels: (id: string) => ['accounts', id, 'models', 'cached'] as const,
}

/** Invalidate everything an account mutation can change. */
export function invalidateAccounts(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: qk.accounts, exact: true }),
    qc.invalidateQueries({ queryKey: qk.status }),
  ])
}

export function useAccounts() {
  return useQuery({
    queryKey: qk.accounts,
    queryFn: async () => (await get<Account[] | null>('/accounts')) ?? [],
    // React Query pauses interval refetches while the tab is hidden.
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  })
}

/* ---------- single-account calls ---------- */

export const accountApi = {
  update: (id: string, patch: AccountPatch) => put<{ success: true }>(`/accounts/${encodeURIComponent(id)}`, patch),
  remove: (id: string) => del<{ success: true }>(`/accounts/${encodeURIComponent(id)}`),
  full: (id: string) => get<AccountFull>(`/accounts/${encodeURIComponent(id)}/full`),
  refresh: (id: string) =>
    post<{ success: true; message?: string; info?: unknown }>(`/accounts/${encodeURIComponent(id)}/refresh`),
  test: (id: string, model: string, signal?: AbortSignal) =>
    api<{ success: true; reply: string; model: string }>(`/accounts/${encodeURIComponent(id)}/test`, {
      method: 'POST',
      json: { model },
      signal,
    }),
  models: async (id: string) =>
    (await get<{ success: true; models: UpstreamModelInfo[] | null }>(`/accounts/${encodeURIComponent(id)}/models`))
      .models ?? [],
  cachedModels: async (id: string) =>
    (await get<{ success: true; models: string[] | null }>(`/accounts/${encodeURIComponent(id)}/models/cached`)).models ??
    [],
  refreshModels: (id: string) =>
    post<{ success: true; count: number }>(`/accounts/${encodeURIComponent(id)}/models/refresh`),
  getOverage: (id: string) => get<OverageResponse>(`/accounts/${encodeURIComponent(id)}/overage`),
  setOverage: (id: string, enabled: boolean) =>
    post<OverageResponse>(`/accounts/${encodeURIComponent(id)}/overage`, { enabled }),
  batch: <T = { success: true; count?: number; refreshed?: number; failed?: number }>(
    ids: string[],
    action: 'enable' | 'disable' | 'refresh',
  ) => post<T>('/accounts/batch', { ids, action }),
  refreshAllModels: () => post<{ success: true; refreshed: number; failed: number }>('/accounts/models/refresh'),
  generateMachineId: async () => (await get<{ machineId: string }>('/generate-machine-id')).machineId,
  export: (ids: string[]) => post<ExportData>('/export', { ids }),
}

export interface ExportAccount {
  id: string
  email: string
  nickname?: string
  idp: string
  userId?: string
  profileArn?: string
  machineId?: string
  credentials: {
    accessToken: string
    csrfToken: string
    refreshToken: string
    clientId?: string
    clientSecret?: string
    region?: string
    expiresAt: number
    authMethod?: string
    provider?: string
    tokenEndpoint?: string
    issuerUrl?: string
    scopes?: string
  }
  subscription: { type: string; title?: string }
  usage: { current: number; limit: number; percentUsed: number; lastUpdated: number }
  tags: string[]
  status: string
  createdAt: number
  lastUsedAt: number
}
export interface ExportData {
  version: string
  exportedAt: number
  accounts: ExportAccount[]
  groups: unknown[]
  tags: unknown[]
}

export function useAccountModels(id: string, enabled: boolean) {
  return useQuery({
    queryKey: accountKeys.models(id),
    queryFn: () => accountApi.models(id),
    enabled,
    staleTime: 5 * 60_000,
    retry: false,
  })
}

export function useCachedAccountModels(id: string, enabled: boolean) {
  return useQuery({
    queryKey: accountKeys.cachedModels(id),
    queryFn: async () => (await accountApi.cachedModels(id)).slice().sort(),
    enabled,
    staleTime: 60_000,
    retry: false,
  })
}

export function useUpdateAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: AccountPatch }) => accountApi.update(id, patch),
    onMutate: async ({ id, patch }) => {
      await qc.cancelQueries({ queryKey: qk.accounts, exact: true })
      const prev = qc.getQueryData<Account[]>(qk.accounts)
      if (prev) qc.setQueryData<Account[]>(qk.accounts, prev.map((a) => (a.id === id ? { ...a, ...patch } : a)))
      return { prev }
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.accounts, ctx.prev)
    },
    onSettled: () => invalidateAccounts(qc),
  })
}

export function useSetOverage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => accountApi.setOverage(id, enabled),
    onSettled: () => invalidateAccounts(qc),
  })
}

export function useSyncOverage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => accountApi.getOverage(id),
    onSettled: () => invalidateAccounts(qc),
  })
}

export function useRefreshAccountModels() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => accountApi.refreshModels(id),
    onSuccess: (_d, id) => {
      qc.invalidateQueries({ queryKey: qk.models })
      qc.invalidateQueries({ queryKey: accountKeys.cachedModels(id) })
    },
  })
}

export function useRefreshAllModels() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => accountApi.refreshAllModels(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.models })
      qc.invalidateQueries({ queryKey: qk.accounts })
    },
  })
}

/* ---------- add-account flows ---------- */

export interface BuilderIdStart {
  sessionId: string
  userCode: string
  verificationUri: string
  interval: number
}
export type BuilderIdPoll =
  | { success: true; completed: false; status: 'pending' | 'slow_down' | string; interval: number }
  | { success: true; completed: true; account: NewAccountRef }

export type MicrosoftComplete =
  | { success: true; stage: 'microsoft'; authorizeUrl: string }
  | { success: true; stage: 'profile'; requiresProfileSelection: true; selectionId: string; profiles: KiroProfile[] }
  | { success: true; stage: 'complete'; account: NewAccountRef; warning?: string }

export const authApi = {
  builderIdStart: (region: string) => post<BuilderIdStart>('/auth/builderid/start', { region }),
  builderIdPoll: (sessionId: string) => post<BuilderIdPoll>('/auth/builderid/poll', { sessionId }),
  iamStart: (startUrl: string, region: string) =>
    post<{ sessionId: string; authorizeUrl: string; expiresIn: number }>('/auth/iam-sso/start', { startUrl, region }),
  iamComplete: (sessionId: string, callbackUrl: string) =>
    post<{ success: true; account: NewAccountRef }>('/auth/iam-sso/complete', { sessionId, callbackUrl }),
  msStart: () =>
    post<{ sessionId: string; authorizeUrl: string; expiresIn: number; stage: string }>('/auth/microsoft-sso/start', {}),
  msComplete: (sessionId: string, callbackUrl: string) =>
    post<MicrosoftComplete>('/auth/microsoft-sso/complete', { sessionId, callbackUrl }),
  msSelectProfile: (selectionId: string, profileArn: string) =>
    post<{ success: true; stage: 'complete'; account: NewAccountRef; warning?: string }>(
      '/auth/microsoft-sso/select-profile',
      { selectionId, profileArn },
    ),
  msCancel: (sessionId?: string, selectionId?: string) =>
    post<{ success: true }>('/auth/microsoft-sso/cancel', { sessionId: sessionId || '', selectionId: selectionId || '' }),
  ssoToken: (bearerToken: string, region: string) =>
    post<{ success: true; accounts: NewAccountRef[] | null; errors: string[] | null }>('/auth/sso-token', {
      bearerToken,
      region,
    }),
  credentials: (body: CredentialImport) => post<{ success: true; account: NewAccountRef }>('/auth/credentials', body),
}
