/** React Query hooks and wire types for model fallbacks, budgets and alerts. */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from './api'

export interface ModelFallback {
  model: string
  fallbacks: string[]
}

export interface DailyUsage {
  day: string
  resetsAt: string
  globalCredits: number
  keyCredits: Record<string, number>
}

export interface PoolUsage {
  accounts: number
  withLimit: number
  saturated: number
  used: number
  limit: number
  remaining: number
  ratio: number
  threshold: number
  nextResetDay: string
}

export type AlertLevel = 'warning' | 'critical'

export interface CostAlert {
  kind: 'global_daily' | 'key_daily' | 'key_credits' | 'key_tokens' | 'pool_usage'
  level: AlertLevel
  subject?: string
  name?: string
  value: number
  limit: number
  text: string
}

export interface BudgetSettings {
  dailyCreditLimit: number
  budgetTimeZone: string
  alertWebhookURL: string
}

export interface Budgets extends BudgetSettings {
  usageRoutingThreshold: number
  thresholdFromEnv: boolean
  today: DailyUsage
  pool: PoolUsage
  alerts: CostAlert[]
}

export type BudgetsPatch = Partial<BudgetSettings> & { usageRoutingThreshold?: number }

export const ck = {
  fallbacks: ['model-fallbacks'] as const,
  budgets: ['budgets'] as const,
  alerts: ['alerts'] as const,
}

export function useModelFallbacks() {
  return useQuery({
    queryKey: ck.fallbacks,
    queryFn: async () => (await get<{ fallbacks: ModelFallback[] | null }>('/model-fallbacks')).fallbacks ?? [],
  })
}

export function useSaveModelFallbacks() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (fallbacks: ModelFallback[]) => post<{ success: true }>('/model-fallbacks', { fallbacks }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ck.fallbacks }),
  })
}

export function useBudgets(refetchInterval: number | false = 30_000) {
  return useQuery({ queryKey: ck.budgets, queryFn: () => get<Budgets>('/budgets'), refetchInterval })
}

export function useSaveBudgets() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (patch: BudgetsPatch) => post<Budgets>('/budgets', patch),
    onSuccess: (data) => {
      qc.setQueryData(ck.budgets, data)
      qc.invalidateQueries({ queryKey: ck.alerts })
    },
  })
}

export function useTestAlertWebhook() {
  return useMutation({ mutationFn: () => post<{ success: true }>('/budgets/test-webhook') })
}

export function useAlerts() {
  return useQuery({
    queryKey: ck.alerts,
    queryFn: async () => (await get<{ alerts: CostAlert[] | null }>('/alerts')).alerts ?? [],
    refetchInterval: 60_000,
  })
}
