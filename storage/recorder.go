package storage

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"kiro-go/telemetry"
)

// Recorder persists request records and audit events asynchronously.
//
// RecordRequest/RecordAudit never block: they hand the record to a bounded
// queue and return. A single background goroutine drains the queue and writes
// in batches (COPY for request logs). If the database is slow or down the
// queue fills and new records are dropped and counted, so inference latency is
// never affected by logging.
type Recorder struct {
	db       *DB
	requests chan *telemetry.RequestRecord
	audits   chan *telemetry.AuditEvent
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once

	written atomic.Uint64
	dropped atomic.Uint64
	failed  atomic.Uint64

	retention func() time.Duration
}

const (
	recorderQueue    = 20000
	recorderBatch    = 500
	recorderInterval = 500 * time.Millisecond
)

// NewRecorder starts the background writer. retention is consulted at each
// hourly prune; nil or a value <= 0 keeps logs forever.
func NewRecorder(db *DB, retention func() time.Duration) *Recorder {
	r := &Recorder{
		db:        db,
		requests:  make(chan *telemetry.RequestRecord, recorderQueue),
		audits:    make(chan *telemetry.AuditEvent, 2000),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		retention: retention,
	}
	go r.loop()
	return r
}

func (r *Recorder) RecordRequest(rec *telemetry.RequestRecord) {
	select {
	case r.requests <- rec:
	default:
		r.dropped.Add(1)
	}
}

func (r *Recorder) RecordAudit(e *telemetry.AuditEvent) {
	select {
	case r.audits <- e:
	default:
		r.dropped.Add(1)
	}
}

func (r *Recorder) SinkStats() telemetry.SinkStats {
	return telemetry.SinkStats{
		Queued:  len(r.requests) + len(r.audits),
		Written: r.written.Load(),
		Dropped: r.dropped.Load(),
		Failed:  r.failed.Load(),
	}
}

// Close flushes what is queued (bounded by ctx) and stops the writer.
func (r *Recorder) Close(ctx context.Context) {
	r.once.Do(func() { close(r.stop) })
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}

func (r *Recorder) loop() {
	defer close(r.done)
	ticker := time.NewTicker(recorderInterval)
	defer ticker.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	reqBuf := make([]*telemetry.RequestRecord, 0, recorderBatch)
	auditBuf := make([]*telemetry.AuditEvent, 0, 64)

	flush := func() {
		if len(reqBuf) > 0 {
			r.writeRequests(reqBuf)
			reqBuf = reqBuf[:0]
		}
		if len(auditBuf) > 0 {
			r.writeAudits(auditBuf)
			auditBuf = auditBuf[:0]
		}
	}
	r.prune()
	for {
		select {
		case rec := <-r.requests:
			reqBuf = append(reqBuf, rec)
			if len(reqBuf) >= recorderBatch {
				flush()
			}
		case e := <-r.audits:
			auditBuf = append(auditBuf, e)
		case <-ticker.C:
			flush()
		case <-prune.C:
			r.prune()
		case <-r.stop:
			for {
				select {
				case rec := <-r.requests:
					reqBuf = append(reqBuf, rec)
				case e := <-r.audits:
					auditBuf = append(auditBuf, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

var requestColumns = []string{"id", "ts", "method", "path", "endpoint", "model", "account_id", "account_email",
	"api_key_id", "client_ip", "user_agent", "stream", "protocol", "status", "http_status", "error", "error_type",
	"input_tokens", "output_tokens", "credits", "duration_ms", "ttfb_ms", "attempts", "request_bytes", "response_bytes"}

func (r *Recorder) writeRequests(batch []*telemetry.RequestRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows := make([][]any, 0, len(batch))
	var bodies [][]any
	for _, x := range batch {
		rows = append(rows, []any{x.ID, x.Time, x.Method, x.Path, x.Endpoint, x.Model, x.AccountID, x.AccountEmail,
			x.APIKeyID, x.ClientIP, clean(x.UserAgent, 512), x.Stream, x.Protocol, x.Status, x.HTTPStatus, clean(x.Error, 8000),
			x.ErrorType, x.InputTokens, x.OutputTokens, x.Credits, x.DurationMs, x.TTFBMs, x.Attempts,
			x.RequestBytes, x.ResponseBytes})
		if x.RequestBody != "" || x.ResponseBody != "" {
			bodies = append(bodies, []any{x.ID, clean(x.RequestBody, 0), clean(x.ResponseBody, 0)})
		}
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err == nil {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"kiro_request_logs"}, requestColumns, pgx.CopyFromRows(rows))
		if err == nil && len(bodies) > 0 {
			_, err = tx.CopyFrom(ctx, pgx.Identifier{"kiro_request_bodies"}, []string{"id", "request_body", "response_body"}, pgx.CopyFromRows(bodies))
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
	}
	if err != nil {
		r.failed.Add(uint64(len(batch)))
		log.Printf("[Recorder] failed to write %d request logs: %v", len(batch), err)
		return
	}
	r.written.Add(uint64(len(batch)))
}

func (r *Recorder) writeAudits(batch []*telemetry.AuditEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &pgx.Batch{}
	for _, e := range batch {
		var detail []byte
		if len(e.Detail) > 0 {
			detail, _ = json.Marshal(e.Detail)
		}
		b.Queue(`INSERT INTO kiro_audit_events (ts, actor, ip, action, target, detail) VALUES ($1,$2,$3,$4,$5,$6)`,
			e.Time, e.Actor, e.IP, e.Action, e.Target, detail)
	}
	if err := r.db.Pool.SendBatch(ctx, b).Close(); err != nil {
		r.failed.Add(uint64(len(batch)))
		log.Printf("[Recorder] failed to write %d audit events: %v", len(batch), err)
		return
	}
	r.written.Add(uint64(len(batch)))
}

func (r *Recorder) prune() {
	if r.retention == nil {
		return
	}
	keep := r.retention()
	if keep <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cutoff := time.Now().Add(-keep)
	if _, err := r.db.Pool.Exec(ctx, `DELETE FROM kiro_request_logs WHERE ts < $1`, cutoff); err != nil {
		log.Printf("[Recorder] prune request logs: %v", err)
	}
	// Audit events are kept twice as long as request logs.
	if _, err := r.db.Pool.Exec(ctx, `DELETE FROM kiro_audit_events WHERE ts < $1`, cutoff.Add(-keep)); err != nil {
		log.Printf("[Recorder] prune audit events: %v", err)
	}
}

// clean makes a value safe for a PostgreSQL TEXT column (valid UTF-8, no NUL
// bytes) and caps its size; one bad value would otherwise fail a whole batch.
func clean(s string, n int) string {
	if n > 0 && len(s) > n {
		s = s[:n]
	}
	s = strings.ToValidUTF8(s, "\uFFFD")
	return strings.ReplaceAll(s, "\x00", "")
}

// ClearRequests deletes all persisted request logs (admin "clear logs").
func (r *Recorder) ClearRequests(ctx context.Context) error {
	_, err := r.db.Pool.Exec(ctx, `TRUNCATE kiro_request_logs, kiro_request_bodies`)
	return err
}
