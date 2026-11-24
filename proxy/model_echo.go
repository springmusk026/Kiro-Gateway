package proxy

import (
	"context"
	"strings"
)

// requestedModelKey carries the model name exactly as the client sent it.
//
// Requests are routed and logged with the resolved Kiro model id (aliases,
// version normalisation, thinking suffix stripped), but the official APIs
// answer with the model the client asked for. Clients such as SDK wrappers and
// cost dashboards key on that echo, so responses report the requested name
// while request traces and telemetry keep the internal id.
type requestedModelKey struct{}

// withRequestedModel records the client's model name on the request context.
func withRequestedModel(ctx context.Context, requested string) context.Context {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ctx
	}
	return context.WithValue(ctx, requestedModelKey{}, requested)
}

// echoModel returns the model name to put in a response body: the client's
// requested name when known, otherwise the resolved id.
func echoModel(ctx context.Context, resolved string) string {
	if ctx != nil {
		if v, ok := ctx.Value(requestedModelKey{}).(string); ok && v != "" {
			return v
		}
	}
	return resolved
}
