/** Helpers for API list prices (models.dev) and the Kiro credit price. */
import type { PricingStatus } from './types'

/** Kiro's plan price per credit, used when the server does not send one. */
export const DEFAULT_CREDIT_PRICE_USD = 0.02

/** True when list prices are loaded, so a $0 cost means "free", not "unknown". */
export const pricesLoaded = (p: PricingStatus | undefined | null) => !!p && p.models > 0

/** Unix seconds of the last price fetch, or 0 (Go sends the zero time when nothing is loaded). */
export function pricesFetchedAt(p: PricingStatus | undefined | null): number {
  const ms = p?.fetchedAt ? Date.parse(p.fetchedAt) : NaN
  return Number.isFinite(ms) && ms > Date.UTC(2000, 0, 1) ? ms / 1000 : 0
}

/** What the credits cost in dollars. */
export const kiroCost = (credits: number, creditPriceUsd: number | undefined) =>
  credits * (creditPriceUsd && creditPriceUsd > 0 ? creditPriceUsd : DEFAULT_CREDIT_PRICE_USD)
