package proxy

import (
	"context"
	"errors"
	"testing"

	"kiro-go/config"
)

func withFakeProtocols(t *testing.T, fn func(protocol string, cb *KiroStreamCallback) error) *[]string {
	t.Helper()
	t.Setenv("KIRO_UPSTREAM_PROTOCOL", "auto")
	calls := &[]string{}
	orig := callProtocolFn
	callProtocolFn = func(protocol string, _ context.Context, _ *config.Account, _ *KiroPayload, cb *KiroStreamCallback) error {
		*calls = append(*calls, protocol)
		return fn(protocol, cb)
	}
	t.Cleanup(func() { callProtocolFn = orig })
	return calls
}

func TestAutoProtocolFallsBackAndRemembers(t *testing.T) {
	acc := &config.Account{ID: "acc-fallback", RefreshToken: "r"}
	accountProtocols.Delete(acc.ID)
	calls := withFakeProtocols(t, func(p string, cb *KiroStreamCallback) error {
		if p == responsesProtocolResponses {
			return errors.New("HTTP 403 from Kiro responses: FEATURE_NOT_SUPPORTED")
		}
		cb.OnText("hi", false)
		return nil
	})
	cb := &KiroStreamCallback{OnText: func(string, bool) {}}
	if err := CallKiroAPIContext(context.Background(), acc, &KiroPayload{}, cb); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1] != responsesProtocolLegacy {
		t.Fatalf("expected responses then legacy, got %v", *calls)
	}
	*calls = nil
	CallKiroAPIContext(context.Background(), acc, &KiroPayload{}, cb)
	if (*calls)[0] != responsesProtocolLegacy {
		t.Fatalf("working protocol should be tried first next time, got %v", *calls)
	}
}

func TestAutoProtocolNeverSwitchesAfterOutput(t *testing.T) {
	acc := &config.Account{ID: "acc-emitted", RefreshToken: "r"}
	accountProtocols.Delete(acc.ID)
	calls := withFakeProtocols(t, func(p string, cb *KiroStreamCallback) error {
		cb.OnText("partial", false)
		return errors.New("HTTP 404 mid-stream")
	})
	err := CallKiroAPIContext(context.Background(), acc, &KiroPayload{}, &KiroStreamCallback{OnText: func(string, bool) {}})
	if err == nil || len(*calls) != 1 {
		t.Fatalf("must not retry on another protocol after output reached the client: calls=%v err=%v", *calls, err)
	}
}

func TestAutoProtocolIgnoresNonProtocolErrors(t *testing.T) {
	acc := &config.Account{ID: "acc-quota", RefreshToken: "r"}
	accountProtocols.Delete(acc.ID)
	calls := withFakeProtocols(t, func(string, *KiroStreamCallback) error {
		return errors.New("HTTP 429 from Kiro responses: quota exhausted")
	})
	CallKiroAPIContext(context.Background(), acc, &KiroPayload{}, &KiroStreamCallback{})
	if len(*calls) != 1 {
		t.Fatalf("quota errors are not protocol rejections: %v", *calls)
	}
}
