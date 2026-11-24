/** Shared helpers for the analytics page. */
import { useMemo } from 'react'
import { usePrivacyMode } from '@/components/accounts/lib'
import { ENDPOINT_LABEL } from '@/components/logs/log-meta'
import { maskEmail } from '@/lib/format'
import type { TFn } from '@/lib/i18n'
import { useApiKeys } from '@/lib/queries.settings'
import type { AnalyticsDimension, AnalyticsFilters } from '@/lib/types'

export type AnalyticsRange = '24h' | '7d' | '30d' | '90d'
export const ANALYTICS_RANGES: Record<AnalyticsRange, number> = { '24h': 24, '7d': 168, '30d': 720, '90d': 2160 }
export const isRange = (v: unknown): v is AnalyticsRange => typeof v === 'string' && v in ANALYTICS_RANGES

export const FILTER_DIMS = ['model', 'account', 'apiKey', 'endpoint'] as const satisfies readonly (keyof AnalyticsFilters & AnalyticsDimension)[]
export type FilterDim = (typeof FILTER_DIMS)[number]

export type AnalyticsSearch = AnalyticsFilters & { range?: AnalyticsRange }

const RANGE_KEY = 'kiro_analytics_range'
export function readStoredRange(): AnalyticsRange {
  try {
    const v = localStorage.getItem(RANGE_KEY)
    if (isRange(v)) return v
  } catch {
    /* storage unavailable */
  }
  return '24h'
}
export function storeRange(r: AnalyticsRange) {
  try {
    localStorage.setItem(RANGE_KEY, r)
  } catch {
    /* storage unavailable */
  }
}

/** Relative change; null when there is nothing to compare against. */
export function change(cur: number, prev: number): number | null {
  if (!prev) return null
  return (cur - prev) / prev
}

export const ratio = (part: number, total: number) => (total > 0 ? part / total : 0)

/** "per hour" / "per day" for a bucket size in seconds. */
export function perBucket(t: TFn, sec: number): string {
  if (sec === 600 || sec === 3600 || sec === 21600 || sec === 86400) return t(`analytics.per.${sec}` as const)
  return t('analytics.per.other', { n: Math.round(sec / 60) })
}

/** Human labels for breakdown keys (account emails honour the shared privacy toggle). */
export function useDimLabels(t: TFn) {
  const [privacy] = usePrivacyMode()
  const keys = useApiKeys()
  const keyNames = useMemo(() => {
    const m: Record<string, string> = {}
    for (const k of keys.data ?? []) m[k.id] = k.name || k.keyMasked
    return m
  }, [keys.data])
  return useMemo(
    () =>
      (dim: AnalyticsDimension, key: string, label?: string): { text: string; title?: string; muted?: boolean } => {
        if (!key) return { text: dim === 'apiKey' ? t('analytics.noKey') : t('analytics.unknown'), muted: true }
        switch (dim) {
          case 'account': {
            const email = label || ''
            if (!email) return { text: key.slice(0, 8), title: key }
            return { text: privacy ? maskEmail(email) : email, title: privacy ? undefined : email }
          }
          case 'apiKey':
            return { text: keyNames[key] || key.slice(0, 8), title: key }
          case 'endpoint':
            return { text: ENDPOINT_LABEL[key] ? t(ENDPOINT_LABEL[key]) : key, title: key }
          default:
            return { text: key, title: key }
        }
      },
    [t, privacy, keyNames],
  )
}

/** Bucket label for the time axis and tooltips. Daily buckets are UTC-midnight aligned, so label them in UTC. */
export function bucketLabel(ts: number, bucketSec: number, lang: string, long = false): string {
  const d = new Date(ts * 1000)
  if (bucketSec >= 86400) return d.toLocaleDateString(lang, { month: 'short', day: 'numeric', weekday: long ? 'short' : undefined, timeZone: 'UTC' })
  if (bucketSec >= 3600 * 6 || long)
    return d.toLocaleString(lang, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return d.toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' })
}
