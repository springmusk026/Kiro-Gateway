package proxy

import (
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func TestTokenNeedsRefresh(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		acc  config.Account
		want bool
	}{
		{"unknown expiry with refresh token", config.Account{AccessToken: "a", RefreshToken: "r"}, true},
		{"unknown expiry without refresh token", config.Account{AccessToken: "a"}, false},
		{"fresh", config.Account{RefreshToken: "r", ExpiresAt: now + 3600}, false},
		{"about to expire", config.Account{RefreshToken: "r", ExpiresAt: now + 10}, true},
		{"api key account", config.Account{AuthMethod: "api_key", KiroApiKey: "k"}, false},
	}
	for _, c := range cases {
		if got := tokenNeedsRefresh(&c.acc); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
