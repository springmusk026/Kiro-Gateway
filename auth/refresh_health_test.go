package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func TestRefreshTokenRecordsOutcome(t *testing.T) {
	ResetRefreshHealthForTest()
	defer ResetRefreshHealthForTest()
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"accessToken":"at","refreshToken":"rt","expiresIn":3600}`))
	}))
	defer srv.Close()
	old := socialTokenURL
	socialTokenURL = func() string { return srv.URL }
	defer func() { socialTokenURL = old }()
	if err := config.InitForTest(t, filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatal(err)
	}
	oldClient := SetGlobalAuthClientForTest(&http.Client{Transport: &http.Transport{}})
	defer SetGlobalAuthClientForTest(oldClient)

	acc := &config.Account{ID: "acc", AuthMethod: "social", RefreshToken: "rt"}
	for i := 0; i < 2; i++ {
		if _, _, _, _, err := RefreshToken(acc); err == nil {
			t.Fatal("expected failure")
		}
	}
	if h := GetRefreshHealth("acc"); h.ConsecutiveFailures != 2 || h.LastError == "" {
		t.Fatalf("after failures: %+v", h)
	}
	fail = false
	if _, _, _, _, err := RefreshToken(acc); err != nil {
		t.Fatal(err)
	}
	if h := GetRefreshHealth("acc"); h.ConsecutiveFailures != 0 || h.LastSuccess.IsZero() {
		t.Fatalf("after success: %+v", h)
	}
}
