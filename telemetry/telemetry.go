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
	ID            string    `json:"id"`
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	Endpoint      string    `json:"endpoint"` // claude | openai | responses | count_tokens | models | admin
	Model         string    `json:"model,omitempty"`
	AccountID     string    `json:"accountId,omitempty"`
	AccountEmail  string    `json:"accountEmail,omitempty"`
	APIKeyID      string    `json:"apiKeyId,omitempty"`
	ClientIP      string    `json:"clientIp,omitempty"`
	UserAgent     string    `json:"userAgent,omitempty"`
	Stream        bool      `json:"stream"`
	Protocol      string    `json:"protocol,omitempty"` // upstream protocol actually used
	Status        string    `json:"status"`             // success | error
	HTTPStatus    int       `json:"httpStatus"`
	Error         string    `json:"error,omitempty"`
	ErrorType     string    `json:"errorType,omitempty"`
	InputTokens   int       `json:"inputTokens"`
	OutputTokens  int       `json:"outputTokens"`
	CacheRead     int       `json:"cacheReadTokens"`  // upstream prompt-cache hits
	CacheWrite    int       `json:"cacheWriteTokens"` // upstream prompt-cache writes
	Credits       float64   `json:"credits"`
	DurationMs    int64     `json:"durationMs"`
	TTFBMs        int64     `json:"ttfbMs"` // time to first byte written to the client
	Attempts      int       `json:"attempts"`
	RequestBytes  int64     `json:"requestBytes"`
	ResponseBytes int64     `json:"responseBytes"`
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
