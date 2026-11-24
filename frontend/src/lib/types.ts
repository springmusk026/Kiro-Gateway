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
  mode: ThinkingMode
  budget: number
}
export type ThinkingMode = 'auto' | 'always' | 'never'

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
  promptCaching?: boolean
  cacheMinTokens?: number
  effortLevels?: string[]
  defaultEffort?: string
  thinkingToggle?: boolean
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

// ---------- Storage, request history and audit (GET /storage, /requests, /usage/*, /audit) ----------

export interface RecorderStats {
  queued: number
  written: number
  dropped: number
  failed: number
}

/** GET /storage */
export interface StorageInfo {
  /** "postgres" or "json:<path>" */
  backend: string
  database: boolean
  logBodiesKB: number
  logRetentionDays: number
  recorder?: RecorderStats
}

export interface LoggingSettings {
  logBodiesKB: number
  logRetentionDays: number
}

export type HistoryEndpoint = 'claude' | 'openai' | 'responses' | 'count_tokens' | (string & {})

/** One persisted request (GET /requests, GET /requests/{id}). */
export interface RequestRecord {
  id: string
  /** RFC3339 */
  time: string
  method: string
  path: string
  endpoint: HistoryEndpoint
  model?: string
  accountId?: string
  accountEmail?: string
  apiKeyId?: string
  clientIp?: string
  userAgent?: string
  stream: boolean
  protocol?: 'responses' | 'legacy' | '' | (string & {})
  status: 'success' | 'error' | (string & {})
  httpStatus: number
  error?: string
  errorType?: RequestLog['errorType']
  inputTokens: number
  outputTokens: number
  credits: number
  durationMs: number
  ttfbMs: number
  attempts: number
  requestBytes: number
  responseBytes: number
  requestBody?: string
  responseBody?: string
}

export interface RequestsPage {
  records: RequestRecord[] | null
  hasMore: boolean
}

export interface RequestFilters {
  status?: string
  endpoint?: string
  model?: string
  account?: string
  apiKey?: string
  errorType?: string
  q?: string
  /** 0 = all time */
  hours?: number
}

export interface UsagePoint {
  /** RFC3339 bucket start */
  time: string
  requests: number
  errors: number
  inputTokens: number
  outputTokens: number
  credits: number
  avgLatencyMs: number
}

export interface UsageSeries {
  points: UsagePoint[] | null
  bucketSeconds: number
}

export type UsageDimension = 'model' | 'account' | 'apiKey' | 'endpoint' | 'client'

export interface UsageGroup {
  key: string
  requests: number
  errors: number
  inputTokens: number
  outputTokens: number
  credits: number
}

export interface AuditEvent {
  /** RFC3339 */
  time: string
  actor: string
  ip?: string
  action: string
  target?: string
  detail?: Record<string, string>
}

export type MigrationState = 'none' | 'pending' | 'migrated' | 'diverged'
export interface MigrationStatus {
  database: boolean
  state: MigrationState
  jsonPath: string
  jsonExists: boolean
  jsonSize: number
  jsonModified?: string
  autoImport: boolean
  jsonAccounts: number
  jsonKeys: number
  jsonResponses: number
  dbAccounts: number
  dbKeys: number
  missingAccounts: number
  missingKeys: number
  missingResponses: number
}
export type MigrationAction = 'import' | 'replace' | 'delete'
export interface MigrationResult {
  action: MigrationAction
  importedAccounts: number
  importedKeys: number
  importedResponses: number
  skipped: number
  verified: boolean
  deleted?: string[]
  status: MigrationStatus
}

// ---- Analytics (GET /admin/api/analytics) ----

export interface AnalyticsSummary {
  requests: number
  errors: number
  /** HTTP 4xx returned to the client (excluding 429) */
  clientErrors: number
  /** HTTP 429/503/529 */
  throttled: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  credits: number
  avgLatencyMs: number
  p50LatencyMs: number
  p95LatencyMs: number
  p99LatencyMs: number
  p50TtfbMs: number
  p95TtfbMs: number
  avgAttempts: number
  /** requests that needed more than one upstream attempt */
  retried: number
  streamed: number
  requestBytes: number
  responseBytes: number
  accounts: number
  apiKeys: number
  models: number
  /** median output throughput of successful requests */
  outputTokensPerSec: number
}

export interface AnalyticsPoint {
  /** RFC3339, bucket start (epoch-aligned) */
  time: string
  requests: number
  errors: number
  throttled: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  credits: number
  p50LatencyMs: number
  p95LatencyMs: number
  p50TtfbMs: number
}

export interface AnalyticsGroup {
  key: string
  /** account email for the account breakdown */
  label?: string
  requests: number
  errors: number
  throttled: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  credits: number
  p50LatencyMs: number
  p95LatencyMs: number
  p50TtfbMs: number
  avgAttempts: number
  /** RFC3339 */
  lastSeen: string
}

export type AnalyticsDimension = 'model' | 'account' | 'apiKey' | 'client' | 'endpoint' | 'protocol' | 'status'

export interface AnalyticsError {
  type: string
  status: number
  count: number
  sample: string
  /** RFC3339 */
  lastSeen: string
}

export interface AnalyticsHeatCell {
  /** 0 = Sunday */
  weekday: number
  hour: number
  requests: number
  errors: number
}

export interface AnalyticsSlow {
  id: string
  /** RFC3339 */
  time: string
  model: string
  endpoint: string
  status: string
  durationMs: number
  ttfbMs: number
  outputTokens: number
  attempts: number
}

export interface AnalyticsReport {
  from: string
  to: string
  bucketSec: number
  summary: AnalyticsSummary
  /** the preceding window of equal length */
  previous: AnalyticsSummary
  series: AnalyticsPoint[] | null
  breakdowns: Partial<Record<AnalyticsDimension, AnalyticsGroup[] | null>> | null
  errors: AnalyticsError[] | null
  heatmap: AnalyticsHeatCell[] | null
  slowest: AnalyticsSlow[] | null
}

export interface AnalyticsFilters {
  model?: string
  account?: string
  apiKey?: string
  endpoint?: string
}
