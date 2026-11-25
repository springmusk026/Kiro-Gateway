package proxy

import (
	"net/http"
	"testing"
)

func TestKiroTransportReusesConnections(t *testing.T) {
	tr := buildKiroTransport("")
	if !tr.ForceAttemptHTTP2 || tr.MaxIdleConnsPerHost < 20 || tr.IdleConnTimeout <= 0 {
		t.Fatalf("connection reuse settings: h2=%v perHost=%d idle=%s", tr.ForceAttemptHTTP2, tr.MaxIdleConnsPerHost, tr.IdleConnTimeout)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.ClientSessionCache == nil {
		t.Fatal("TLS sessions must be cached for resumption")
	}
	if tr.DialContext == nil || tr.TLSHandshakeTimeout <= 0 || tr.ResponseHeaderTimeout <= 0 {
		t.Fatal("dial, handshake and response-header waits must be bounded")
	}
	if tr.HTTP2 == nil || tr.HTTP2.SendPingTimeout <= 0 {
		t.Fatal("silent HTTP/2 connections must be health-checked")
	}

	InitKiroHttpClient("")
	t.Cleanup(func() { InitKiroHttpClient("") })
	if kiroHttpStore.Load().Transport != kiroRestHttpStore.Load().Transport {
		t.Fatal("streaming and REST clients should share one transport")
	}
	const proxy = "http://proxy.example:3128"
	a, b := GetClientForProxy(proxy), GetRestClientForProxy(proxy)
	if a.Transport.(*http.Transport) != b.Transport.(*http.Transport) {
		t.Fatal("per-proxy clients should share one transport")
	}
}
