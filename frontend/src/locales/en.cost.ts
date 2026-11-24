/** English strings for model fallbacks, budgets, alerts and pool usage. Keys start with 'cost.'. */
export const costEn = {
  // Model fallbacks (Models → Aliases)
  'cost.fallbacks': 'Model fallbacks',
  'cost.fallbacksDesc':
    'When a model is unavailable or overloaded upstream, retry the request once on its first fallback. Client errors, quota and rate limits never fall back.',
  'cost.fallbacksEmpty': 'No fallbacks. Requests for an unavailable model fail with the upstream error.',
  'cost.fbModel': 'Model',
  'cost.fbFallbacks': 'Fallbacks, in order',
  'cost.fbFallbacksPlaceholder': 'model-a, model-b',
  'cost.fbAdd': 'Add fallback',
  'cost.fbRequired': 'Required',
  'cost.fbDuplicate': 'Listed twice',
  'cost.fbInvalid': 'No spaces or control characters',
  'cost.fbSelf': 'A model cannot fall back to itself',
  'cost.fbTooMany': 'At most 5 fallbacks per model',
  'cost.fbFix': 'Fix the highlighted rows to save',
  'cost.fbHint':
    'Only the first fallback is tried, once per request, before anything is streamed. The response carries an x-kiro-go-fallback-model header and the request log shows the fallback.',
  'cost.fbUnknown': 'Not in the current catalog',
  'cost.fbLogLabel': 'Fallback model',

  // Settings: routing threshold
  'cost.routing': 'Usage-aware routing',
  'cost.routingDesc': 'Prefer accounts with credits left; spare the ones close to their limit.',
  'cost.threshold': 'Skip accounts above',
  'cost.thresholdHint':
    'Accounts at or above this share of their limit are used only when no other account is available. Conversations stay on their account until it crosses the threshold.',
  'cost.thresholdInvalid': 'Enter a percentage between 1 and 100',
  'cost.thresholdEnv': 'Set by KIRO_USAGE_THRESHOLD; the environment variable wins over this setting.',

  // Settings: budgets
  'cost.secBudgets': 'Budgets & alerts',
  'cost.secBudgetsDesc': 'Daily credit caps and where alerts are sent.',
  'cost.budgets': 'Daily credit budget',
  'cost.budgetsDesc':
    'Requests are rejected with 429 once the cap is reached, until midnight in the budget time zone. Per-key daily caps are set on each API key.',
  'cost.dailyLimit': 'Daily cap (credits)',
  'cost.unlimited': 'No cap',
  'cost.limitInvalid': 'Enter a positive number',
  'cost.timeZone': 'Budget time zone',
  'cost.timeZoneHint': 'IANA name such as Europe/Berlin. Empty means UTC.',
  'cost.todayUsage': 'Used today: {used} credits',
  'cost.todayUsageOf': 'Used today: {used} of {limit} credits',
  'cost.resets': 'Resets {when}',
  'cost.alerts': 'Alerts',
  'cost.alertsDesc':
    'At 80% and 100% of a daily cap or key limit, and at 90% of the pool’s account limits. Shown in the console and posted to the webhook once per level per day.',
  'cost.webhook': 'Webhook URL',
  'cost.webhookHint': 'Optional. Receives JSON with a "text" field (Slack and Discord compatible), plus kind, level, value and limit.',
  'cost.webhookInvalid': 'Enter an http(s) URL',
  'cost.testWebhook': 'Send test',
  'cost.testSent': 'Test alert sent',
  'cost.activeAlerts': 'Active alerts',
  'cost.noAlerts': 'Nothing over 80% right now.',

  // Banner
  'cost.bannerTitle': 'Budget alerts',
  'cost.bannerCritical': '{n} limit(s) reached',
  'cost.bannerWarning': '{n} limit(s) close to their cap',
  'cost.manage': 'Manage budgets',

  // Accounts: pool usage
  'cost.poolTitle': 'Credits remaining this period',
  'cost.poolRemaining': '{remaining} of {limit} left',
  'cost.poolUsed': '{pct}% used across {n} account(s) with a known limit',
  'cost.poolSaturated': '{n} above the routing threshold ({pct}%)',
  'cost.poolReset': 'Next reset {date}',
  'cost.poolUnknown': 'No usage reported yet. Refresh accounts to load their limits.',
  'cost.left': '{n} left',

  // Keys
  'cost.dailyKeyLimit': 'Daily credit cap',
  'cost.dailyKeyHint': 'Resets at midnight in the budget time zone (Settings → Budgets & alerts).',
  'cost.keyToday': 'Today: {used} / {limit}',
} as const
