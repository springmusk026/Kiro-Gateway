// Package telemetry defines the request/audit records the gateway produces and
// the interfaces that persist them. It has no dependencies so both the proxy
// and the storage backends can import it.
package telemetry

import (
	"context"
	"time"
)

// RequestRecord is one client request as seen by the gateway.
type RequestRecord struct {
	ID           string    `json:"id"`
	Time         time.Time `json:"time"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Endpoint     string    `json:"endpoint"` // claude | openai | responses | count_tokens | models | admin
	Model        string    `json:"model,omitempty"`
	AccountID    string    `json:"accountId,omitempty"`
	AccountEmail string    `json:"accountEmail,omitempty"`
	APIKeyID     string    `json:"apiKeyId,omitempty"`
	ClientIP     string    `json:"clientIp,omitempty"`
	UserAgent    string    `json:"userAgent,omitempty"`
	Stream       bool      `json:"stream"`
	Protocol     string    `json:"protocol,omitempty"` // upstream protocol actually used
	Status       string    `json:"status"`             // success | error
	HTTPStatus   int       `json:"httpStatus"`
	Error        string    `json:"error,omitempty"`
	ErrorType    string    `json:"errorType,omitempty"`
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
	CacheRead    int       `json:"cacheReadTokens"`  // upstream prompt-cache hits
	CacheWrite   int       `json:"cacheWriteTokens"` // upstream prompt-cache writes
	Credits      float64   `json:"credits"`
	// CostUSD is what the request would have cost at the model vendor's API
	// list price (models.dev), 0 when the model's price is unknown.
	CostUSD       float64 `json:"costUsd"`
	DurationMs    int64   `json:"durationMs"`
	TTFBMs        int64   `json:"ttfbMs"` // time to first byte written to the client
	Attempts      int     `json:"attempts"`
	RequestBytes  int64   `json:"requestBytes"`
	ResponseBytes int64   `json:"responseBytes"`

	// FallbackModel is the model the request was retried on after its own
	// model was unavailable (empty when no fallback was taken).
	FallbackModel string `json:"fallbackModel,omitempty"`

	// ConversationID is the upstream conversation id ("sess_…") the request
	// was sent under; turns of one conversation share it.
	ConversationID string `json:"conversationId,omitempty"`

	// Optional captured bodies (only when body capture is enabled).
	RequestBody  string `json:"requestBody,omitempty"`
	ResponseBody string `json:"responseBody,omitempty"`
}

// AuditEvent records an administrative action or security-relevant event.
type AuditEvent struct {
	Time   time.Time         `json:"time"`
	Actor  string            `json:"actor"` // admin | system
	IP     string            `json:"ip,omitempty"`
	Action string            `json:"action"` // e.g. account.add, settings.update, login.failed
	Target string            `json:"target,omitempty"`
	Detail map[string]string `json:"detail,omitempty"`
}

// Sink accepts records without blocking the caller. Implementations buffer
// and write asynchronously, dropping (and counting) records when overloaded.
type Sink interface {
	RecordRequest(r *RequestRecord)
	RecordAudit(e *AuditEvent)
}

// LogFilter selects request records.
type LogFilter struct {
	Since, Until time.Time
	Status       string
	Endpoint     string
	Model        string
	AccountID    string
	APIKeyID     string
	ErrorType    string
	Search       string
	BeforeID     string // keyset pagination: records older than this id
	Limit        int
}

// UsagePoint is one bucket of a usage time series.
type UsagePoint struct {
	Time         time.Time `json:"time"`
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheRead    int64     `json:"cacheReadTokens"`
	Credits      float64   `json:"credits"`
	AvgLatencyMs float64   `json:"avgLatencyMs"`
}

// UsageGroup aggregates usage by a dimension (model, account, API key).
type UsageGroup struct {
	Key          string  `json:"key"`
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CacheRead    int64   `json:"cacheReadTokens"`
	Credits      float64 `json:"credits"`
}

// Reader queries persisted records (available with a database backend).
type Reader interface {
	QueryRequests(ctx context.Context, f LogFilter) ([]RequestRecord, error)
	// CountRequests counts matches (cursor ignored), capped; capped=true means more.
	CountRequests(ctx context.Context, f LogFilter) (n int64, capped bool, err error)
	GetRequest(ctx context.Context, id string) (*RequestRecord, error)
	UsageSeries(ctx context.Context, since time.Time, bucket time.Duration) ([]UsagePoint, error)
	UsageBy(ctx context.Context, dimension string, since time.Time, limit int) ([]UsageGroup, error)
	QueryAudit(ctx context.Context, since time.Time, limit int) ([]AuditEvent, error)
	SinkStats() SinkStats
}

// SinkStats reports the async writer's health.
type SinkStats struct {
	Queued  int    `json:"queued"`
	Written uint64 `json:"written"`
	Dropped uint64 `json:"dropped"`
	Failed  uint64 `json:"failed"`
}

// ResponseStore persists stored /v1/responses objects (opaque JSON documents).
type ResponseStore interface {
	SaveResponse(ctx context.Context, id string, doc []byte, storedAt time.Time) error
	LoadResponse(ctx context.Context, id string) ([]byte, time.Time, error)
	PurgeResponses(ctx context.Context, olderThan time.Time, keepMax int) (int, error)
}

// MigrationStatus describes a leftover config.json next to a PostgreSQL
// backend, compared against what the database holds.
type MigrationStatus struct {
	Database    bool      `json:"database"`
	State       string    `json:"state"` // none | pending | migrated | diverged
	JSONPath    string    `json:"jsonPath"`
	JSONExists  bool      `json:"jsonExists"`
	JSONSize    int64     `json:"jsonSize"`
	JSONModTime time.Time `json:"jsonModified,omitempty"`
	AutoImport  bool      `json:"autoImport"`

	JSONAccounts  int `json:"jsonAccounts"`
	JSONKeys      int `json:"jsonKeys"`
	JSONResponses int `json:"jsonResponses"`
	DBAccounts    int `json:"dbAccounts"`
	DBKeys        int `json:"dbKeys"`

	MissingAccounts  int `json:"missingAccounts"`
	MissingKeys      int `json:"missingKeys"`
	MissingResponses int `json:"missingResponses"`
}

// MigrationResult reports one migration action.
type MigrationResult struct {
	Action            string           `json:"action"`
	ImportedAccounts  int              `json:"importedAccounts"`
	ImportedKeys      int              `json:"importedKeys"`
	ImportedResponses int              `json:"importedResponses"`
	Skipped           int              `json:"skipped"`
	Verified          bool             `json:"verified"`
	Deleted           []string         `json:"deleted,omitempty"`
	Status            *MigrationStatus `json:"status"`
}

// Migrator moves a config.json (and stored responses) into the database.
type Migrator interface {
	Status(ctx context.Context) (*MigrationStatus, error)
	// Run performs "import" (fill an empty database or merge what is
	// missing), "replace" (overwrite the database with the JSON) or "delete"
	// (remove the JSON files once everything is verified in the database).
	// With deleteAfter, import/replace also delete after verification.
	Run(ctx context.Context, action string, deleteAfter bool) (*MigrationResult, error)
}

// AnalyticsQuery selects the window and optional filters for Analytics.
type AnalyticsQuery struct {
	From, To time.Time
	Bucket   time.Duration
	Location *time.Location // for the weekday × hour heatmap
	Model    string
	Account  string
	APIKey   string
	Endpoint string
}

// AnalyticsSummary aggregates one window.
type AnalyticsSummary struct {
	Requests       int64   `json:"requests"`
	Errors         int64   `json:"errors"`
	Cancelled      int64   `json:"cancelled"`    // the client disconnected before the response finished
	ClientErrors   int64   `json:"clientErrors"` // HTTP 4xx returned to the client
	Throttled      int64   `json:"throttled"`    // HTTP 429/529/503
	InputTokens    int64   `json:"inputTokens"`
	OutputTokens   int64   `json:"outputTokens"`
	CacheRead      int64   `json:"cacheReadTokens"`
	CacheWrite     int64   `json:"cacheWriteTokens"`
	Credits        float64 `json:"credits"`
	CostUSD        float64 `json:"costUsd"` // API list-price equivalent
	AvgLatencyMs   float64 `json:"avgLatencyMs"`
	P50LatencyMs   float64 `json:"p50LatencyMs"`
	P95LatencyMs   float64 `json:"p95LatencyMs"`
	P99LatencyMs   float64 `json:"p99LatencyMs"`
	P50TTFBMs      float64 `json:"p50TtfbMs"`
	P95TTFBMs      float64 `json:"p95TtfbMs"`
	AvgAttempts    float64 `json:"avgAttempts"`
	Retried        int64   `json:"retried"` // requests that needed more than one upstream attempt
	Streamed       int64   `json:"streamed"`
	RequestBytes   int64   `json:"requestBytes"`
	ResponseBytes  int64   `json:"responseBytes"`
	Accounts       int64   `json:"accounts"`
	APIKeys        int64   `json:"apiKeys"`
	Models         int64   `json:"models"`
	OutputTokensPS float64 `json:"outputTokensPerSec"` // median output throughput of successful requests
}

// AnalyticsPoint is one time bucket.
type AnalyticsPoint struct {
	Time         time.Time `json:"time"`
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	Cancelled    int64     `json:"cancelled"`
	Throttled    int64     `json:"throttled"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheRead    int64     `json:"cacheReadTokens"`
	CacheWrite   int64     `json:"cacheWriteTokens"`
	Credits      float64   `json:"credits"`
	CostUSD      float64   `json:"costUsd"` // API list-price equivalent
	P50LatencyMs float64   `json:"p50LatencyMs"`
	P95LatencyMs float64   `json:"p95LatencyMs"`
	P50TTFBMs    float64   `json:"p50TtfbMs"`
}

// AnalyticsGroup is one row of a breakdown.
type AnalyticsGroup struct {
	Key          string    `json:"key"`
	Label        string    `json:"label,omitempty"`
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	Throttled    int64     `json:"throttled"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheRead    int64     `json:"cacheReadTokens"`
	CacheWrite   int64     `json:"cacheWriteTokens"`
	Credits      float64   `json:"credits"`
	CostUSD      float64   `json:"costUsd"` // API list-price equivalent
	P50LatencyMs float64   `json:"p50LatencyMs"`
	P95LatencyMs float64   `json:"p95LatencyMs"`
	P50TTFBMs    float64   `json:"p50TtfbMs"`
	AvgAttempts  float64   `json:"avgAttempts"`
	LastSeen     time.Time `json:"lastSeen"`
}

// AnalyticsError groups failures by class and HTTP status.
type AnalyticsError struct {
	Type     string    `json:"type"`
	Status   int       `json:"status"`
	Count    int64     `json:"count"`
	Sample   string    `json:"sample"`
	LastSeen time.Time `json:"lastSeen"`
}

// AnalyticsHeatCell counts requests per local weekday (0 = Sunday) and hour.
type AnalyticsHeatCell struct {
	Weekday  int   `json:"weekday"`
	Hour     int   `json:"hour"`
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
}

// AnalyticsSlow is one of the slowest requests in the window.
type AnalyticsSlow struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Model      string    `json:"model"`
	Endpoint   string    `json:"endpoint"`
	Status     string    `json:"status"`
	DurationMs int64     `json:"durationMs"`
	TTFBMs     int64     `json:"ttfbMs"`
	Output     int64     `json:"outputTokens"`
	Attempts   int       `json:"attempts"`
}

// AnalyticsReport is everything the analytics page shows for one window.
type AnalyticsReport struct {
	From       time.Time                   `json:"from"`
	To         time.Time                   `json:"to"`
	BucketSec  int64                       `json:"bucketSec"`
	Summary    AnalyticsSummary            `json:"summary"`
	Previous   AnalyticsSummary            `json:"previous"` // the preceding window of equal length
	Series     []AnalyticsPoint            `json:"series"`
	Breakdowns map[string][]AnalyticsGroup `json:"breakdowns"`
	Errors     []AnalyticsError            `json:"errors"`
	Heatmap    []AnalyticsHeatCell         `json:"heatmap"`
	Slowest    []AnalyticsSlow             `json:"slowest"`
	// Set by the API layer: the price of a Kiro credit and the list-price
	// catalogue the costUsd figures come from.
	CreditPriceUSD float64     `json:"creditPriceUsd"`
	Pricing        interface{} `json:"pricing,omitempty"`
}

// AnalyticsReader is implemented by backends that can aggregate history.
type AnalyticsReader interface {
	Analytics(ctx context.Context, q AnalyticsQuery) (*AnalyticsReport, error)
}
