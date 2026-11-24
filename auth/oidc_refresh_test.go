package auth

import (
	"testing"
	"time"
)

func TestFinishRefreshRejectsEmptyAccessToken(t *testing.T) {
	if _, _, _, _, err := finishRefresh("  ", "rt", 3600, ""); err == nil {
		t.Fatal("empty access token must be an error")
	}
	_, _, exp, _, err := finishRefresh("at", "rt", 0, "")
	if err != nil || exp < time.Now().Unix()+3000 {
		t.Fatalf("missing expiresIn should default to ~1h, got exp=%d err=%v", exp, err)
	}
}
