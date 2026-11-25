/** English strings for API list prices and cost. Keys start with 'pricing.'. */
export const pricingEn = {
  // Shared
  'pricing.listPrice': 'API list price',
  'pricing.kiroCost': 'Kiro cost',
  'pricing.saved': 'Saved',
  'pricing.costTitle': 'What the same tokens cost at the model vendor’s API list price',

  // Analytics: cost strip
  'pricing.listHint': '{n} per request',
  'pricing.kiroHint': '{credits} credits × {price}',
  'pricing.savedHint': '{pct} of list price',
  'pricing.overHint': '{pct} more than list price',
  'pricing.source': 'List prices from models.dev, updated {ago}',
  'pricing.notLoaded': 'List prices have not loaded yet; they are fetched from models.dev at start and once a day.',
  'pricing.off': 'List prices are turned off (KIRO_PRICING=off).',
  'pricing.note':
    'List price is what the same tokens would cost on the model vendor’s own API. Cache reads and writes are priced only when Kiro reports them; models without a list price count as $0. Kiro cost is credits × the credit price set in Settings.',
  'pricing.creditPriceLink': 'Credit price',

  // Analytics: chart and breakdown
  'pricing.chart': 'Cost',
  'pricing.chartHint': 'API list price and Kiro cost {per}',
  'pricing.col': 'List price',

  // Models catalog
  'pricing.colPrice': 'List price',
  'pricing.perM': 'in / out per 1M tokens',
  'pricing.inOut': 'Input {in} · output {out} per 1M tokens',
  'pricing.cacheRead': 'Cache read {v}',
  'pricing.cacheWrite': 'Cache write {v}',
  'pricing.tier': 'Above {n} tokens: {in} / {out}',
  'pricing.from': 'models.dev: {id}',
  'pricing.none': 'No list price on models.dev',
  'pricing.catalogSource': 'List prices: models.dev, updated {ago}',

  // Settings
  'pricing.creditPrice': 'Credit price (USD)',
  'pricing.creditPriceHint': 'What one Kiro credit costs you. Kiro’s plans include credits at $0.02 each; overage is billed separately.',
  'pricing.creditPriceInvalid': 'Enter a price between 0 and 100',

  // Key-holder usage page
  'pricing.usageTotal': 'API list price of these 14 days: {v}',
} as const
