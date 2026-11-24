package proxy

import (
	"errors"
	"io"
	"testing"
	"time"
)

type blockingBody struct{ closed chan struct{} }

func (b *blockingBody) Read(p []byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestIdleTimeoutBodyUnblocksSilentStream(t *testing.T) {
	body := &blockingBody{closed: make(chan struct{})}
	g := newIdleTimeoutBody(body, 50*time.Millisecond)
	start := time.Now()
	_, err := g.Read(make([]byte, 8))
	if !errors.Is(err, errStreamIdleTimeout) {
		t.Fatalf("want idle timeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("idle timeout did not fire promptly")
	}
	if !isRetryableStreamError(err) {
		t.Fatal("an idle stream that sent nothing must stay retryable")
	}
}

func TestReadUpstreamStreamClosesBodyOnPanic(t *testing.T) {
	body := &blockingBody{closed: make(chan struct{})}
	func() {
		defer func() { recover() }()
		readUpstreamStream(body, func(io.Reader) (bool, error) { panic("callback failed") })
	}()
	select {
	case <-body.closed:
	default:
		t.Fatal("body must be closed when the parser panics")
	}
}
