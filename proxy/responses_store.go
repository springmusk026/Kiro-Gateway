package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/telemetry"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	responsesDirName    = "responses"
	responsesDefaultTTL = 30 * 24 * time.Hour
)

func responsesDir() string {
	return filepath.Join(config.GetConfigDir(), responsesDirName)
}

func generateResponseID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("resp_%d%012x", time.Now().UnixNano(), 0)
	}
	return "resp_" + hex.EncodeToString(buf) + fmt.Sprintf("%08x", time.Now().Unix()&0xffffffff)
}

func generateOutputItemID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

func saveResponse(resp *ResponsesObject) error {
	if resp == nil || resp.ID == "" {
		return fmt.Errorf("response missing id")
	}
	if responseBackend != nil {
		return saveResponseDB(resp)
	}
	dir := responsesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create responses dir: %w", err)
	}
	if resp.StoredAt == 0 {
		resp.StoredAt = time.Now().Unix()
	}

	persisted := toStoredDoc(resp)

	path := filepath.Join(dir, sanitizeResponseID(resp.ID)+".json")
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stored response: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write stored response: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit stored response: %w", err)
	}
	return nil
}

func loadResponse(id string) (*ResponsesObject, error) {
	if id == "" {
		return nil, fmt.Errorf("empty response id")
	}
	if responseBackend != nil {
		return loadResponseDB(id)
	}
	path := filepath.Join(responsesDir(), sanitizeResponseID(id)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc storedResponseDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode stored response: %w", err)
	}
	if doc.StoredAt > 0 && time.Since(time.Unix(doc.StoredAt, 0)) > responsesDefaultTTL {
		_ = os.Remove(path)
		return nil, fmt.Errorf("stored response expired")
	}
	return fromStoredDoc(doc), nil
}

// Bounds for the on-disk Responses store, so stored conversations cannot
// fill the data volume (which would also break saving config.json).
// Override with KIRO_MAX_STORED_RESPONSES / KIRO_MAX_STORED_RESPONSES_MB.
func storedResponsesLimits() (maxFiles int, maxBytes int64) {
	maxFiles, maxBytes = 20000, 2<<30
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_MAX_STORED_RESPONSES"))); err == nil && v > 0 {
		maxFiles = v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_MAX_STORED_RESPONSES_MB"))); err == nil && v > 0 {
		maxBytes = int64(v) << 20
	}
	return
}

// StoredResponseTTL is how long stored Responses objects are kept before the
// periodic purge removes them.
func StoredResponseTTL() time.Duration { return responsesDefaultTTL }

// purgeExpiredResponses removes stored responses older than ttl, then evicts
// the oldest ones until the store is within the count and size limits.
func purgeExpiredResponses(ttl time.Duration) {
	if ttl <= 0 {
		ttl = responsesDefaultTTL
	}
	if responseBackend != nil {
		maxFiles, _ := storedResponsesLimits()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if n, err := responseBackend.PurgeResponses(ctx, time.Now().Add(-ttl), maxFiles); err != nil {
			logger.Warnf("[Responses] purge failed: %v", err)
		} else if n > 0 {
			logger.Infof("[Responses] purged %d stored responses", n)
		}
		return
	}
	dir := responsesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type stored struct {
		path string
		mod  time.Time
		size int64
	}
	cutoff := time.Now().Add(-ttl)
	kept := make([]stored, 0, len(entries))
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(full); err != nil {
				logger.Warnf("[Responses] purge %s failed: %v", e.Name(), err)
			}
			continue
		}
		kept = append(kept, stored{full, info.ModTime(), info.Size()})
		total += info.Size()
	}
	maxFiles, maxBytes := storedResponsesLimits()
	if len(kept) <= maxFiles && total <= maxBytes {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].mod.Before(kept[j].mod) })
	evicted := 0
	for _, f := range kept {
		if len(kept)-evicted <= maxFiles && total <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
			evicted++
		}
	}
	logger.Infof("[Responses] evicted %d oldest stored responses to stay within limits", evicted)
}

// responsesStoreJanitor applies the store limits periodically, not just at
// startup, so long-running processes stay bounded.
func responsesStoreJanitor(stop <-chan struct{}) {
	purgeExpiredResponses(responsesDefaultTTL)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			purgeExpiredResponses(responsesDefaultTTL)
		case <-stop:
			return
		}
	}
}

func logResponsesPersistFailure(id string, err error) {
	logger.Warnf("[Responses] persist %s failed: %v", id, err)
}

func sanitizeResponseID(id string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-':
			return r
		default:
			return -1
		}
	}, id)
	if cleaned == "" {
		return "invalid"
	}
	return cleaned
}

type storedResponseDoc struct {
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	CreatedAt          int64                `json:"created_at"`
	Status             string               `json:"status"`
	Model              string               `json:"model"`
	Output             []ResponseOutputItem `json:"output"`
	Usage              ResponsesUsage       `json:"usage"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
	Instructions       string               `json:"instructions,omitempty"`
	StoredInput        json.RawMessage      `json:"stored_input,omitempty"`
	StoredAt           int64                `json:"stored_at"`

	IncompleteDetails *ResponsesIncompleteDetails `json:"incomplete_details,omitempty"`
	Error             *ResponsesError             `json:"error,omitempty"`
	Tools             json.RawMessage             `json:"tools,omitempty"`
	ToolChoice        json.RawMessage             `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                       `json:"parallel_tool_calls,omitempty"`
	Temperature       *float64                    `json:"temperature,omitempty"`
	TopP              *float64                    `json:"top_p,omitempty"`
	MaxOutputTokens   *int                        `json:"max_output_tokens,omitempty"`
	Truncation        string                      `json:"truncation,omitempty"`
}

func fromStoredDoc(doc storedResponseDoc) *ResponsesObject {
	return &ResponsesObject{
		ID:                 doc.ID,
		Object:             doc.Object,
		CreatedAt:          doc.CreatedAt,
		Status:             doc.Status,
		Model:              doc.Model,
		Output:             doc.Output,
		Usage:              doc.Usage,
		PreviousResponseID: doc.PreviousResponseID,
		Metadata:           doc.Metadata,
		Instructions:       doc.Instructions,
		StoredInput:        doc.StoredInput,
		StoredAt:           doc.StoredAt,
		IncompleteDetails:  doc.IncompleteDetails,
		Error:              doc.Error,
		Tools:              doc.Tools,
		ToolChoice:         doc.ToolChoice,
		ParallelToolCalls:  doc.ParallelToolCalls,
		Temperature:        doc.Temperature,
		TopP:               doc.TopP,
		MaxOutputTokens:    doc.MaxOutputTokens,
		Truncation:         doc.Truncation,
	}
}

// responseBackend stores /v1/responses objects in the database when set.
var responseBackend telemetry.ResponseStore

func toStoredDoc(resp *ResponsesObject) storedResponseDoc {
	return storedResponseDoc{
		ID:                 resp.ID,
		Object:             resp.Object,
		CreatedAt:          resp.CreatedAt,
		Status:             resp.Status,
		Model:              resp.Model,
		Output:             resp.Output,
		Usage:              resp.Usage,
		PreviousResponseID: resp.PreviousResponseID,
		Metadata:           resp.Metadata,
		Instructions:       resp.Instructions,
		StoredInput:        resp.StoredInput,
		StoredAt:           resp.StoredAt,
		IncompleteDetails:  resp.IncompleteDetails,
		Error:              resp.Error,
		Tools:              resp.Tools,
		ToolChoice:         resp.ToolChoice,
		ParallelToolCalls:  resp.ParallelToolCalls,
		Temperature:        resp.Temperature,
		TopP:               resp.TopP,
		MaxOutputTokens:    resp.MaxOutputTokens,
		Truncation:         resp.Truncation,
	}
}

// responseDeleter is implemented by database stores that can delete one
// stored response.
type responseDeleter interface {
	DeleteResponse(ctx context.Context, id string) (bool, error)
}

// deleteResponse removes a stored response. It reports whether one existed.
func deleteResponse(id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	if responseBackend != nil {
		d, ok := responseBackend.(responseDeleter)
		if !ok {
			return false, fmt.Errorf("response store does not support deletion")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return d.DeleteResponse(ctx, id)
	}
	path := filepath.Join(responsesDir(), sanitizeResponseID(id)+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func saveResponseDB(resp *ResponsesObject) error {
	if resp.StoredAt == 0 {
		resp.StoredAt = time.Now().Unix()
	}
	data, err := json.Marshal(toStoredDoc(resp))
	if err != nil {
		return fmt.Errorf("marshal stored response: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return responseBackend.SaveResponse(ctx, resp.ID, data, time.Unix(resp.StoredAt, 0))
}

func loadResponseDB(id string) (*ResponsesObject, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, storedAt, err := responseBackend.LoadResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if time.Since(storedAt) > responsesDefaultTTL {
		return nil, fmt.Errorf("stored response expired")
	}
	var doc storedResponseDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode stored response: %w", err)
	}
	return fromStoredDoc(doc), nil
}
