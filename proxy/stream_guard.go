package proxy

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errStreamIdleTimeout reports an upstream stream that stopped sending data.
// It is deliberately not a net.Error timeout, so a stream that went silent
// before anything reached the client is still retried.
var errStreamIdleTimeout = errors.New("upstream stream idle timeout: no data received")

// streamIdleTimeout is the longest gap allowed between two reads of an
// upstream stream. Long thinking turns keep sending frames, so this replaces
// the old 5-minute cap on the whole response (which cut long answers off).
// Override with KIRO_STREAM_IDLE_TIMEOUT (seconds).
func streamIdleTimeout() time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_STREAM_IDLE_TIMEOUT"))); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return 180 * time.Second
}

// idleTimeoutBody closes the wrapped body when no data arrives for d, which
// unblocks a pending Read; that Read then reports errStreamIdleTimeout.
type idleTimeoutBody struct {
	body     io.ReadCloser
	d        time.Duration
	timer    *time.Timer
	timedOut atomic.Bool
	once     sync.Once
}

func newIdleTimeoutBody(body io.ReadCloser, d time.Duration) *idleTimeoutBody {
	b := &idleTimeoutBody{body: body, d: d}
	b.timer = time.AfterFunc(d, func() {
		b.timedOut.Store(true)
		b.body.Close()
	})
	return b
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if b.timedOut.Load() {
		return n, errStreamIdleTimeout
	}
	if n > 0 {
		b.timer.Reset(b.d)
	}
	return n, err
}

func (b *idleTimeoutBody) Close() error {
	var err error
	b.once.Do(func() {
		b.timer.Stop()
		err = b.body.Close()
	})
	return err
}

// readUpstreamStream runs parse over a guarded body and always closes it,
// even when a callback panics (the panic is recovered by net/http and would
// otherwise leak the upstream connection).
func readUpstreamStream[T any](body io.ReadCloser, parse func(io.Reader) (T, error)) (T, error) {
	guarded := newIdleTimeoutBody(body, streamIdleTimeout())
	defer guarded.Close()
	return parse(guarded)
}
