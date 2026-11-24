/** Wire types for the Kiro-Go admin API (see backend proxy/handler.go). */

export type AuthMethod = 'idc' | 'social' | 'external_idp' | 'api_key' | (string & {})
export type BanStatus = '' | 'ACTIVE' | 'BANNED' | 'DISABLED' | 'SUSPENDED' | (string & {})
export type OverageStatus = '' | 'ENABLED' | 'DISABLED' | 'UNKNOWN' | (string & {})

/** GET /accounts element — secrets stripped. */
export interface Account {
  id: string
  email: string
  userId: string
  nickname: string
  authMethod: AuthMethod
  provider: string
  region: string
  enabled: boolean
  banStatus: BanStatus
  banReason: string
  banTime: number
  expiresAt: number
  hasToken: boolean
  machineId: string
  weight: number
  overageStatus: OverageStatus
  overageCapability: string
  overageCap: number
  overageRate: number
  currentOverages: number
  overageCheckedAt: number
  proxyURL: string
  subscriptionType: string
  subscriptionTitle: string
  daysRemaining: number
  usageCurrent: number
  usageLimit: number
  /** fraction 0..1 */
  usagePercent: number
  nextResetDate: string
  lastRefresh: number
  trialUsageCurrent: number
  trialUsageLimit: number
  /** fraction 0..1 */
  trialUsagePercent: number
  trialStatus: string
  trialExpiresAt: number
  requestCount: number
  errorCount: number
  totalTokens: number
  totalCredits: number
  lastUsed: number
}

/** GET /accounts/{id}/full — includes secrets. */
export interface AccountFull extends Omit<Account, 'hasToken'> {
  accessToken: string
  refreshToken: string
  clientId: string
  clientSecret: string
  profileArn: string
  tokenEndpoint: string
  issuerUrl: string
  scopes: string
}

export interface AccountPatch {
  enabled?: boolean
  nickname?: string
  machineId?: string
  weight?: number
  proxyURL?: string
}

export interface NewAccountRef {
  id: string
  email: string
}

export interface UpstreamModelInfo {
  modelId: string
  modelName: string
  description: string
  supportedInputTypes: string[] | null
  rateMultiplier: number
  tokenLimits: { maxInputTokens: number; maxOutputTokens: number } | null
}

export interface KiroProfile {
  arn: string
  name: string
  region: string
}

export interface Status {
  version: string
  accounts: number
  available: number
  totalRequests: number
  successRequests: number
  failedRequests: number
  totalTokens: number
  totalCredits: number
  uptime: number
  apiOpen: boolean
}

export interface Settings {
  apiKey: string
  requireApiKey: boolean
  port: number
  host: string
  allowOverUsage: boolean
}
export interface SettingsPatch {
  requireApiKey?: boolean
  password?: string
  allowOverUsage?: boolean
}

export type ThinkingFormat = 'reasoning_content' | 'thinking' | 'think'
export interface ThinkingConfig {
  suffix: string
  openaiFormat: ThinkingFormat
  claudeFormat: ThinkingFormat
}

export interface EndpointConfig {
  preferredEndpoint: 'auto' | 'kiro' | 'codewhisperer' | 'amazonq'
  endpointFallback: boolean
  upstreamProtocol: 'auto' | 'responses' | 'legacy'
}

export interface ProxyConfig {
  proxyURL: string
}

export interface ModelMapping {
  source: string
  target: string
}

export interface PromptFilterRule {
  id: string
  name: string
  type: 'regex' | 'lines-containing'
  match: string
  replace?: string
  enabled: boolean
}
export interface PromptFilterConfig {
  filterClaudeCode: boolean
  filterEnvNoise: boolean
  filterStripBoundaries: boolean
  rules: PromptFilterRule[]
}

export interface CatalogModel {
  id: string
  name?: string
  description?: string
  inputTypes?: string[]
  contextLength?: number
  maxOutputTokens?: number
  rateMultiplier?: number
  vision: boolean
  enabled: boolean
  accounts: number
}
export interface ModelsCatalog {
  models: CatalogModel[]
  cachedAt: number
  ttlSeconds: number
  thinkingSuffix: string
  mappings: ModelMapping[]
  disabledModels: string[]
}

export interface OverageResponse {
  success: true
  overageStatus: OverageStatus
  overageCapability: string
  subscriptionTitle: string
  overageCap: number
  overageRate: number
  currentOverages: number
  overageCheckedAt: number
}

export interface CredentialImport {
  id?: string
  email?: string
  userId?: string
  nickname?: string
  profileArn?: string
  accessToken?: string
  refreshToken?: string
  kiroApiKey?: string
  clientId?: string
  clientSecret?: string
  authMethod?: string
  provider?: string
  region?: string
  tokenEndpoint?: string
  issuerUrl?: string
  scopes?: string
}

export interface ApiKeyView {
  id: string
  name?: string
  keyMasked: string
  enabled: boolean
  migrated?: boolean
  createdAt: number
  lastUsedAt?: number
  tokenLimit?: number
  creditLimit?: number
  tokensUsed: number
  creditsUsed: number
  requestsCount: number
}

export interface RequestLog {
  time: number
  endpoint: string
  model: string
  accountId: string
  status: 'success' | 'error'
  error: string
  errorType: '' | 'quota' | 'overage' | 'suspended' | 'auth' | 'profile' | 'unknown' | (string & {})
  tokens: number
  credits: number
  duration: number
}
