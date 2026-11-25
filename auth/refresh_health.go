package auth

import (
	"sync"
	"time"
)

// RefreshHealth is the in-memory record of an account's token refreshes
// since start-up (account health alerts read it; it is never persisted).
type RefreshHealth struct {
	ConsecutiveFailures int
	LastSuccess         time.Time
	LastFailure         time.Time
	LastError           string
}

var (
	refreshHealthMu sync.Mutex
	refreshHealth   = map[string]RefreshHealth{}
)

func recordRefreshResult(accountID string, err error, now time.Time) {
	if accountID == "" {
		return
	}
	refreshHealthMu.Lock()
	defer refreshHealthMu.Unlock()
	h := refreshHealth[accountID]
	if err == nil {
		h.ConsecutiveFailures = 0
		h.LastSuccess = now
		h.LastError = ""
	} else {
		h.ConsecutiveFailures++
		h.LastFailure = now
		h.LastError = err.Error()
	}
	refreshHealth[accountID] = h
}

// GetRefreshHealth returns the refresh record of an account (zero when no
// refresh was attempted since start-up).
func GetRefreshHealth(accountID string) RefreshHealth {
	refreshHealthMu.Lock()
	defer refreshHealthMu.Unlock()
	return refreshHealth[accountID]
}

// RecordRefreshResultForTest records a refresh outcome. Test helper for other
// packages.
func RecordRefreshResultForTest(accountID string, err error, at time.Time) {
	recordRefreshResult(accountID, err, at)
}

// ResetRefreshHealthForTest forgets every record. Test helper.
func ResetRefreshHealthForTest() {
	refreshHealthMu.Lock()
	refreshHealth = map[string]RefreshHealth{}
	refreshHealthMu.Unlock()
}
