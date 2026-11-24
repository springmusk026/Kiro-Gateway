package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kiro-go/config"
	"kiro-go/telemetry"
)

// Prometheus metrics at GET /metrics, in the text exposition format (written
// by hand: no client library). Off unless KIRO_METRICS=1. With
// KIRO_METRICS_TOKEN set, scrapers must send "Authorization: Bearer <token>";
// without it only loopback and private-network clients may scrape.
//
// Request metrics are collected in finishTrace: one mutex-protected update
// per request, after the response has been written.

const (
	// maxModelLabels caps distinct model label values; later models are
	// folded into "other" so a client cannot blow up cardinality.
	maxModelLabels = 40
	maxErrorLabels = 30
)

// durationBuckets are the histogram upper bounds in seconds (duration, TTFB).
var durationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300}

var metricsEnabledOverride atomic.Int32 // tests: 0 = env, 1 = on, 2 = off

// metricsEnabled reports whether /metrics is served and requests are counted.
func metricsEnabled() bool {
	switch metricsEnabledOverride.Load() {
	case 1:
		return true
	case 2:
		return false
	}
	return metricsEnvEnabled()
}

var metricsEnvEnabled = sync.OnceValue(func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KIRO_METRICS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
})

type histogram struct {
	counts []uint64 // per bucket (non-cumulative), len(durationBuckets)+1
	sum    float64
	n      uint64
}

func (h *histogram) observe(v float64) {
	if h.counts == nil {
		h.counts = make([]uint64, len(durationBuckets)+1)
	}
	i := sort.SearchFloat64s(durationBuckets, v)
	h.counts[i]++
	h.sum += v
	h.n++
}

type reqKey struct{ endpoint, status, model string }

type modelUsage struct {
	input, output, cacheRead, cacheWrite int64
	credits                              float64
}

type metricsRegistry struct {
	mu        sync.Mutex
	requests  map[reqKey]uint64
	attempts  map[string]uint64 // by endpoint
	errors    map[[2]string]uint64
	usage     map[string]*modelUsage
	duration  map[string]*histogram // by endpoint
	ttfb      map[string]*histogram
	models    map[string]bool // admitted model labels
	errLabels map[string]bool

	inFlight atomic.Int64
	start    time.Time
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		requests:  map[reqKey]uint64{},
		attempts:  map[string]uint64{},
		errors:    map[[2]string]uint64{},
		usage:     map[string]*modelUsage{},
		duration:  map[string]*histogram{},
		ttfb:      map[string]*histogram{},
		models:    map[string]bool{},
		errLabels: map[string]bool{},
		start:     time.Now(),
	}
}

var metrics = newMetricsRegistry()

// labelFor returns v if it is admitted (or there is room), else "other".
func labelFor(seen map[string]bool, v string, limit int) string {
	if seen[v] {
		return v
	}
	if len(seen) >= limit {
		return "other"
	}
	seen[v] = true
	return v
}

// observe records one finished request.
func (m *metricsRegistry) observe(rec *telemetry.RequestRecord) {
	endpoint := rec.Endpoint
	if endpoint == "" {
		endpoint = "unknown"
	}
	model := strings.ToLower(strings.TrimSpace(rec.Model))
	if model == "" {
		model = "none"
	}
	status := strconv.Itoa(rec.HTTPStatus)
	if rec.HTTPStatus == 0 {
		status = "0"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	model = labelFor(m.models, model, maxModelLabels)
	m.requests[reqKey{endpoint, status, model}]++
	if rec.Attempts > 0 {
		m.attempts[endpoint] += uint64(rec.Attempts)
	}
	if rec.Status == "error" || rec.HTTPStatus >= 400 {
		class := rec.ErrorType
		if class == "" {
			class = "http_" + status
		}
		class = labelFor(m.errLabels, class, maxErrorLabels)
		m.errors[[2]string{endpoint, class}]++
	}
	u := m.usage[model]
	if u == nil {
		u = &modelUsage{}
		m.usage[model] = u
	}
	u.input += int64(rec.InputTokens)
	u.output += int64(rec.OutputTokens)
	u.cacheRead += int64(rec.CacheRead)
	u.cacheWrite += int64(rec.CacheWrite)
	u.credits += rec.Credits
	hd := m.duration[endpoint]
	if hd == nil {
		hd = &histogram{}
		m.duration[endpoint] = hd
	}
	hd.observe(float64(rec.DurationMs) / 1000)
	if rec.TTFBMs > 0 {
		ht := m.ttfb[endpoint]
		if ht == nil {
			ht = &histogram{}
			m.ttfb[endpoint] = ht
		}
		ht.observe(float64(rec.TTFBMs) / 1000)
	}
}

// promWriter writes the text exposition format.
type promWriter struct {
	w io.Writer
}

func (p promWriter) header(name, typ, help string) {
	fmt.Fprintf(p.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return strings.ReplaceAll(v, `"`, `\"`)
}

// labels renders {k="v",…} from alternating key/value pairs.
func labels(kv ...string) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(kv[i])
		b.WriteString(`="`)
		b.WriteString(escapeLabel(kv[i+1]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func (p promWriter) sample(name, lbls string, v float64) {
	fmt.Fprintf(p.w, "%s%s %s\n", name, lbls, formatFloat(v))
}

func (p promWriter) histogram(name, help string, hs map[string]*histogram) {
	p.header(name, "histogram", help)
	for _, ep := range sortedKeys(hs) {
		h := hs[ep]
		var cum uint64
		for i, le := range durationBuckets {
			cum += h.counts[i]
			p.sample(name+"_bucket", labels("endpoint", ep, "le", formatFloat(le)), float64(cum))
		}
		p.sample(name+"_bucket", labels("endpoint", ep, "le", "+Inf"), float64(h.n))
		p.sample(name+"_sum", labels("endpoint", ep), h.sum)
		p.sample(name+"_count", labels("endpoint", ep), float64(h.n))
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// write renders the request metrics.
func (m *metricsRegistry) write(p promWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p.header("kiro_requests_total", "counter", "API requests by endpoint, HTTP status and model.")
	keys := make([]reqKey, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.endpoint != b.endpoint {
			return a.endpoint < b.endpoint
		}
		if a.status != b.status {
			return a.status < b.status
		}
		return a.model < b.model
	})
	for _, k := range keys {
		p.sample("kiro_requests_total", labels("endpoint", k.endpoint, "status", k.status, "model", k.model), float64(m.requests[k]))
	}

	p.header("kiro_upstream_attempts_total", "counter", "Upstream chat calls made (retries and account switches included).")
	for _, ep := range sortedKeys(m.attempts) {
		p.sample("kiro_upstream_attempts_total", labels("endpoint", ep), float64(m.attempts[ep]))
	}

	p.header("kiro_request_errors_total", "counter", "Failed API requests by endpoint and error class.")
	errKeys := make([][2]string, 0, len(m.errors))
	for k := range m.errors {
		errKeys = append(errKeys, k)
	}
	sort.Slice(errKeys, func(i, j int) bool {
		if errKeys[i][0] != errKeys[j][0] {
			return errKeys[i][0] < errKeys[j][0]
		}
		return errKeys[i][1] < errKeys[j][1]
	})
	for _, k := range errKeys {
		p.sample("kiro_request_errors_total", labels("endpoint", k[0], "class", k[1]), float64(m.errors[k]))
	}

	models := sortedKeys(m.usage)
	p.header("kiro_tokens_total", "counter", "Tokens by model and direction.")
	for _, mo := range models {
		p.sample("kiro_tokens_total", labels("model", mo, "direction", "input"), float64(m.usage[mo].input))
		p.sample("kiro_tokens_total", labels("model", mo, "direction", "output"), float64(m.usage[mo].output))
	}
	p.header("kiro_cache_tokens_total", "counter", "Upstream prompt-cache tokens by model and kind (read, write).")
	for _, mo := range models {
		p.sample("kiro_cache_tokens_total", labels("model", mo, "kind", "read"), float64(m.usage[mo].cacheRead))
		p.sample("kiro_cache_tokens_total", labels("model", mo, "kind", "write"), float64(m.usage[mo].cacheWrite))
	}
	p.header("kiro_credits_total", "counter", "Kiro credits consumed by model.")
	for _, mo := range models {
		p.sample("kiro_credits_total", labels("model", mo), m.usage[mo].credits)
	}

	p.histogram("kiro_request_duration_seconds", "Time from request start to the end of the response.", m.duration)
	p.histogram("kiro_time_to_first_byte_seconds", "Time from request start to the first response byte.", m.ttfb)
}

// accountStates counts configured accounts by routing state.
func (h *Handler) accountStates() map[string]int {
	out := map[string]int{"available": 0, "cooling": 0, "disabled": 0, "exhausted": 0}
	var cooling map[string]bool
	inPool := map[string]bool{}
	if h.pool != nil {
		cooling = h.pool.CoolingIDs()
		for _, a := range h.pool.GetAllAccounts() {
			inPool[a.ID] = true
		}
	}
	for _, a := range config.GetAccounts() {
		banned := a.BanStatus != "" && !strings.EqualFold(a.BanStatus, "ACTIVE")
		switch {
		case !a.Enabled || banned:
			out["disabled"]++
		case !inPool[a.ID]:
			// Enabled but left out of rotation: over quota without overage.
			out["exhausted"]++
		case cooling[a.ID]:
			out["cooling"]++
		default:
			out["available"]++
		}
	}
	return out
}

// writeMetrics renders every metric family.
func (h *Handler) writeMetrics(w io.Writer) {
	p := promWriter{w}
	p.header("kiro_build_info", "gauge", "Build information.")
	p.sample("kiro_build_info", labels("version", config.Version, "goversion", runtime.Version()), 1)
	p.header("kiro_start_time_seconds", "gauge", "Start time of the process (Unix seconds).")
	p.sample("kiro_start_time_seconds", "", float64(metrics.start.Unix()))
	p.header("kiro_requests_in_flight", "gauge", "API requests currently being served.")
	p.sample("kiro_requests_in_flight", "", float64(metrics.inFlight.Load()))

	states := h.accountStates()
	p.header("kiro_accounts", "gauge", "Configured accounts by state.")
	for _, s := range []string{"available", "cooling", "disabled", "exhausted"} {
		p.sample("kiro_accounts", labels("state", s), float64(states[s]))
	}

	if h.logReader != nil {
		st := h.logReader.SinkStats()
		p.header("kiro_recorder_queue_length", "gauge", "Request records waiting to be written to the database.")
		p.sample("kiro_recorder_queue_length", "", float64(st.Queued))
		p.header("kiro_recorder_written_total", "counter", "Request records written to the database.")
		p.sample("kiro_recorder_written_total", "", float64(st.Written))
		p.header("kiro_recorder_dropped_total", "counter", "Request records dropped because the queue was full.")
		p.sample("kiro_recorder_dropped_total", "", float64(st.Dropped))
		p.header("kiro_recorder_failed_total", "counter", "Request records that failed to write.")
		p.sample("kiro_recorder_failed_total", "", float64(st.Failed))
	}
	metrics.write(p)
}

// metricsAccessAllowed applies the token or the private-network rule.
func metricsAccessAllowed(r *http.Request) (bool, int) {
	if token := strings.TrimSpace(os.Getenv("KIRO_METRICS_TOKEN")); token != "" {
		got := ""
		if auth := strings.TrimSpace(r.Header.Get("Authorization")); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			got = strings.TrimSpace(auth[7:])
		}
		a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(token))
		if got == "" || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
			return false, http.StatusUnauthorized
		}
		return true, 0
	}
	ip := net.ParseIP(clientIP(r))
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return true, 0
	}
	return false, http.StatusForbidden
}

// handleMetrics serves GET /metrics.
func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !metricsEnabled() {
		h.sendAPINotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.sendAPIMethodNotAllowed(w, r, "GET")
		return
	}
	if ok, status := metricsAccessAllowed(r); !ok {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	h.writeMetrics(w)
}
