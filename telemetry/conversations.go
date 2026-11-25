package telemetry

import (
	"context"
	"time"
)

// ConversationSummary aggregates the requests of one upstream conversation.
type ConversationSummary struct {
	ID           string    `json:"id"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	Requests     int64     `json:"requests"`
	Models       []string  `json:"models"`
	Accounts     []string  `json:"accounts"` // account ids
	APIKeys      []string  `json:"apiKeys"`
	UserAgent    string    `json:"userAgent,omitempty"` // of the first request
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheRead    int64     `json:"cacheReadTokens"`
	Credits      float64   `json:"credits"`
	Errors       int64     `json:"errors"`
	Cancelled    int64     `json:"cancelled"`
}

// ConversationFilter selects conversations by their latest request time.
type ConversationFilter struct {
	Since    time.Time
	Search   string // conversation id, model or account email (substring)
	BeforeID string // keyset pagination: conversations last seen before this one
	Limit    int
}

// KeyUsageDay is one day of an API key's usage (in the budget time zone).
type KeyUsageDay struct {
	Day          string  `json:"day"` // YYYY-MM-DD
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Credits      float64 `json:"credits"`
}

// ConversationReader lists conversations and their requests.
type ConversationReader interface {
	QueryConversations(ctx context.Context, f ConversationFilter) ([]ConversationSummary, error)
	ConversationRequests(ctx context.Context, id string, limit int) ([]RequestRecord, error)
}

// KeyUsageReader reports one API key's usage per day since a time.
type KeyUsageReader interface {
	KeyDailyUsage(ctx context.Context, keyID string, since time.Time, loc *time.Location) ([]KeyUsageDay, error)
}

// RequestExporter streams the request records matching a filter (newest
// first, at most max rows, without bodies) to fn.
type RequestExporter interface {
	ExportRequests(ctx context.Context, f LogFilter, max int, fn func(*RequestRecord) error) error
}
