package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// A 4xx is deterministic: the official clients never retry it, so it must
// not be replayed on the other endpoints.
func TestLegacyClientErrorIsNotRetriedOnOtherEndpoints(t *testing.T) {
	mustInitConfig(t)
	config.UpdatePreferredEndpoint("auto")
	config.UpdateEndpointFallback(true)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"Improperly formed request","reason":"INVALID_MODEL_ID"}`))
	}))
	defer server.Close()
	old := kiroEndpoints
	kiroEndpoints = []kiroEndpoint{{URL: server.URL, Name: "a"}, {URL: server.URL, Name: "b"}, {URL: server.URL, Name: "c"}}
	oldClient := kiroHttpStore.Load()
	kiroHttpStore.Store(&http.Client{Timeout: time.Second, Transport: &http.Transport{}})
	defer func() { kiroEndpoints = old; kiroHttpStore.Store(oldClient) }()

	acc := &config.Account{ID: "acc", AccessToken: "t", ProfileArn: "arn:aws:codewhisperer:us-east-1:1:profile/x"}
	err := callKiroLegacyAPIContext(context.Background(), acc, newKiroRetryTestPayload(), &KiroStreamCallback{})
	if err == nil {
		t.Fatal("expected the 400 to be returned")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("a client error must not be replayed: %d upstream calls", n)
	}
}
