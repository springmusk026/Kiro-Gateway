/** English strings for key restrictions and console sessions. Keys start with 'keysec.' or 'session.'. */
export const securityEn = {
  // API key restrictions (key dialog + keys list)
  'keysec.title': 'Access restrictions',
  'keysec.allowedModels': 'Allowed models',
  'keysec.allowedModelsHint': 'Empty allows every model. Aliases resolve first; type a prefix ending in * (e.g. claude-sonnet-*) to allow a family.',
  'keysec.allModels': 'All models',
  'keysec.modelsSelected': '{n} selected',
  'keysec.searchModels': 'Search or type a pattern…',
  'keysec.noModels': 'No matching models',
  'keysec.addPattern': 'Allow “{pattern}”',
  'keysec.removeModel': 'Remove {model}',
  'keysec.rpm': 'Requests per minute',
  'keysec.rpmError': 'Whole number ≥ 0',
  'keysec.expiry': 'Expiry',
  'keysec.expires': 'Expires',
  'keysec.never': 'Never',
  'keysec.dateError': 'Pick an expiry date',
  'keysec.badgeExpired': 'Expired',
  'keysec.badgeExpires': 'Expires {date}',
  'keysec.badgeRpm': '{n} RPM',
  'keysec.badgeModels': '{n} models',
  'keysec.badgeModelsHint': 'Only these models: {list}',
  'keysec.expiredHint': 'This key expired on {date}; requests get 401.',

  // Console sessions
  'session.signedOut': 'Signed out',
  'session.expired': 'Your session expired. Sign in again.',
} as const
