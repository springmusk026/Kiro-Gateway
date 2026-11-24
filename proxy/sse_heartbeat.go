package proxy

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// sseHeartbeatInterval is how often an idle stream gets a keep-alive frame.
// Long first-token latencies (large prompts, extended thinking) otherwise let
// proxies and client read timeouts drop the connection.
var sseHeartbeatInterval = 15 * time.Second

const (
	claudePingFrame   = "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	sseKeepAliveFrame = ": keep-alive\n\n"
)

// heartbeatWriter serializes every write to a streaming response so a
// background ticker can interleave keep-alive frames between complete events.
// Each SSE event is written with a single Write call, so locking per Write is
// enough to keep events intact.
type heartbeatWriter struct {
	http.ResponseWriter
	flusher http.Flusher

	mu        sync.Mutex
	committed bool
	stopped   bool
	lastWrite time.Time

	done   chan struct{}
	exited chan struct{}
	once   sync.Once
}

// startSSEHeartbeat wraps w and starts a ticker that writes frame whenever the
// stream has been silent for a full interval. Callers must call Stop before
// returning from the handler.
func startSSEHeartbeat(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, frame string) *heartbeatWriter {
	hw := &heartbeatWriter{
		ResponseWriter: w,
		flusher:        flusher,
		lastWrite:      time.Now(),
		done:           make(chan struct{}),
		exited:         make(chan struct{}),
	}
	interval := sseHeartbeatInterval
	go func() {
		defer close(hw.exited)
		if interval <= 0 {
			<-hw.done
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hw.done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				hw.mu.Lock()
				if hw.stopped {
					hw.mu.Unlock()
					return
				}
				if time.Since(hw.lastWrite) >= interval-interval/10 {
					_, _ = hw.ResponseWriter.Write([]byte(frame))
					if hw.flusher != nil {
						hw.flusher.Flush()
					}
					hw.committed = true
					hw.lastWrite = time.Now()
				}
				hw.mu.Unlock()
			}
		}
	}()
	return hw
}

func (hw *heartbeatWriter) Write(p []byte) (int, error) {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	hw.committed = true
	hw.lastWrite = time.Now()
	return hw.ResponseWriter.Write(p)
}

func (hw *heartbeatWriter) WriteHeader(code int) {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	hw.committed = true
	hw.ResponseWriter.WriteHeader(code)
}

func (hw *heartbeatWriter) Flush() {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	if hw.flusher != nil {
		hw.flusher.Flush()
	}
}

func (hw *heartbeatWriter) Unwrap() http.ResponseWriter { return hw.ResponseWriter }

// Committed reports whether the response status has been sent, after which an
// error can only be reported in-stream.
func (hw *heartbeatWriter) Committed() bool {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	return hw.committed
}

// Stop ends the ticker and waits for it, so nothing is written after the
// handler returns.
func (hw *heartbeatWriter) Stop() {
	hw.once.Do(func() {
		hw.mu.Lock()
		hw.stopped = true
		hw.mu.Unlock()
		close(hw.done)
		<-hw.exited
	})
}
