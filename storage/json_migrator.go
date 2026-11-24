package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kiro-go/config"
	"kiro-go/telemetry"
)

// JSONMigrator lets the admin console finish a move from config.json to
// PostgreSQL: it compares the file with the database, imports or merges what
// is missing, verifies every account, API key and stored response arrived,
// and only then deletes the JSON files.
type JSONMigrator struct {
	DB         *DB
	ConfigPath string
	AutoImport bool
	// ResponseTTL is the stored-response retention. Files older than it are
	// purged right after import, so they never count as missing.
	ResponseTTL time.Duration
	// Reload re-reads the configuration from the database after the whole
	// configuration was replaced (re-applies env overrides, reloads the pool).
	Reload func() error

	mu sync.Mutex
}

var _ telemetry.Migrator = (*JSONMigrator)(nil)

var ErrNotVerified = errors.New("the database does not yet hold everything from config.json; import it first")

func (m *JSONMigrator) responsesDir() string {
	return filepath.Join(filepath.Dir(m.ConfigPath), "responses")
}

// jsonResponseFiles lists stored-response files (id -> path).
func (m *JSONMigrator) jsonResponseFiles() map[string]string {
	all, _ := m.responseFiles()
	return all
}

// liveResponseFiles lists the files still inside the retention window; older
// ones would be purged from the database anyway and need no transfer.
func (m *JSONMigrator) liveResponseFiles() map[string]string {
	_, live := m.responseFiles()
	return live
}

func responseStoredAt(path string) time.Time {
	var meta struct {
		StoredAt int64 `json:"stored_at"`
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &meta)
	}
	if meta.StoredAt > 0 {
		return time.Unix(meta.StoredAt, 0)
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Now()
}

func (m *JSONMigrator) responseFiles() (all, live map[string]string) {
	all = m.rawResponseFiles()
	if m.ResponseTTL <= 0 {
		return all, all
	}
	cutoff := time.Now().Add(-m.ResponseTTL)
	live = make(map[string]string, len(all))
	for id, p := range all {
		if responseStoredAt(p).After(cutoff) {
			live[id] = p
		}
	}
	return all, live
}

func (m *JSONMigrator) rawResponseFiles() map[string]string {
	out := map[string]string{}
	entries, err := os.ReadDir(m.responsesDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = filepath.Join(m.responsesDir(), e.Name())
	}
	return out
}

// jsonKeys returns the API keys a config.json grants, including a legacy
// single key that config.Load would promote.
func jsonKeys(c *config.Config) []config.ApiKeyEntry {
	keys := append([]config.ApiKeyEntry(nil), c.ApiKeys...)
	if len(keys) == 0 && strings.TrimSpace(c.ApiKey) != "" {
		keys = append(keys, config.ApiKeyEntry{Key: c.ApiKey, Enabled: true})
	}
	return keys
}

func (m *JSONMigrator) Status(ctx context.Context) (*telemetry.MigrationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status(ctx)
}

func (m *JSONMigrator) status(ctx context.Context) (*telemetry.MigrationStatus, error) {
	st := &telemetry.MigrationStatus{Database: true, JSONPath: m.ConfigPath, AutoImport: m.AutoImport, State: "none"}
	files := m.jsonResponseFiles()
	st.JSONResponses = len(files)

	var src *config.Config
	if info, err := os.Stat(m.ConfigPath); err == nil {
		st.JSONExists, st.JSONSize, st.JSONModTime = true, info.Size(), info.ModTime()
		c, err := config.ReadJSONFile(m.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", m.ConfigPath, err)
		}
		src = c
	}
	if src == nil && len(files) == 0 {
		return st, nil
	}

	cur, err := config.Snapshot()
	if err != nil {
		return nil, err
	}
	st.DBAccounts, st.DBKeys = len(cur.Accounts), len(cur.ApiKeys)
	// A database without accounts or keys only holds the defaults written at
	// startup, so importing replaces it wholesale (settings included).
	fresh := st.DBAccounts == 0 && st.DBKeys == 0

	if src != nil {
		st.JSONAccounts = len(src.Accounts)
		have := map[string]bool{}
		for _, a := range cur.Accounts {
			have[a.ID] = true
		}
		for _, a := range src.Accounts {
			if !have[a.ID] {
				st.MissingAccounts++
			}
		}
		haveKey := map[string]bool{}
		for _, k := range cur.ApiKeys {
			haveKey[k.Key] = true
		}
		keys := jsonKeys(src)
		st.JSONKeys = len(keys)
		for _, k := range keys {
			if !haveKey[k.Key] {
				st.MissingKeys++
			}
		}
	}
	if live := m.liveResponseFiles(); len(live) > 0 {
		ids := make([]string, 0, len(live))
		for id := range live {
			ids = append(ids, id)
		}
		var present int
		if err := m.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM kiro_responses WHERE id = ANY($1)`, ids).Scan(&present); err != nil {
			return nil, err
		}
		st.MissingResponses = len(ids) - present
	}

	switch {
	case fresh && src != nil && (len(src.Accounts) > 0 || len(jsonKeys(src)) > 0):
		st.State = "pending"
	case st.MissingAccounts+st.MissingKeys+st.MissingResponses == 0:
		st.State = "migrated"
	default:
		st.State = "diverged"
	}
	return st, nil
}

func (m *JSONMigrator) Run(ctx context.Context, action string, deleteAfter bool) (*telemetry.MigrationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := &telemetry.MigrationResult{Action: action}

	before, err := m.status(ctx)
	if err != nil {
		return nil, err
	}
	switch action {
	case "import":
		if before.State == "pending" {
			if err := m.replace(ctx, res); err != nil {
				return nil, err
			}
			break
		}
		if before.JSONExists {
			src, err := config.ReadJSONFile(m.ConfigPath)
			if err != nil {
				return nil, err
			}
			if src != nil {
				merged := *src
				merged.ApiKeys = jsonKeys(src)
				a, k, err := config.MergeMissing(&merged)
				if err != nil {
					return nil, err
				}
				res.ImportedAccounts, res.ImportedKeys = a, k
			}
		}
		n, skipped, err := m.importResponses(ctx, false)
		if err != nil {
			return nil, err
		}
		res.ImportedResponses, res.Skipped = n, skipped
	case "replace":
		if !before.JSONExists {
			return nil, fmt.Errorf("%s does not exist", m.ConfigPath)
		}
		if err := m.replace(ctx, res); err != nil {
			return nil, err
		}
	case "delete":
		deleteAfter = true
	default:
		return nil, fmt.Errorf("unknown action %q (use import, replace or delete)", action)
	}

	after, err := m.status(ctx)
	if err != nil {
		return nil, err
	}
	res.Status = after
	res.Verified = after.State == "migrated" || after.State == "none"
	if deleteAfter {
		if !res.Verified {
			return res, ErrNotVerified
		}
		deleted, err := m.deleteJSON()
		res.Deleted = deleted
		if err != nil {
			return res, err
		}
		if res.Status, err = m.status(ctx); err != nil {
			return res, err
		}
	}
	return res, nil
}

// replace overwrites the database configuration with config.json.
func (m *JSONMigrator) replace(ctx context.Context, res *telemetry.MigrationResult) error {
	ir, err := ImportJSON(ctx, m.DB, m.ConfigPath, true)
	if err != nil {
		return err
	}
	res.ImportedAccounts, res.ImportedKeys, res.ImportedResponses, res.Skipped = ir.Accounts, ir.APIKeys, ir.Responses, ir.Skipped
	if m.Reload != nil {
		return m.Reload()
	}
	return nil
}

// importResponses copies stored responses; existing rows are kept unless overwrite.
func (m *JSONMigrator) importResponses(ctx context.Context, overwrite bool) (n, skipped int, err error) {
	q := `INSERT INTO kiro_responses (id, data, stored_at) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`
	if overwrite {
		q = `INSERT INTO kiro_responses (id, data, stored_at) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, stored_at = EXCLUDED.stored_at`
	}
	for id, path := range m.liveResponseFiles() {
		data, err := os.ReadFile(path)
		if err != nil || !json.Valid(data) {
			skipped++
			continue
		}
		var meta struct {
			StoredAt int64 `json:"stored_at"`
		}
		_ = json.Unmarshal(data, &meta)
		at := time.Unix(meta.StoredAt, 0)
		if meta.StoredAt == 0 {
			at = time.Now()
			if info, err := os.Stat(path); err == nil {
				at = info.ModTime()
			}
		}
		tag, err := m.DB.Pool.Exec(ctx, q, id, data, at)
		if err != nil {
			return n, skipped, err
		}
		if tag.RowsAffected() > 0 {
			n++
		}
	}
	return n, skipped, nil
}

// deleteJSON removes config.json, its leftover temp files and the stored
// response files (the responses directory itself goes when it ends up empty).
// Nothing else in the data directory (e.g. the PostgreSQL volume) is touched.
func (m *JSONMigrator) deleteJSON() ([]string, error) {
	var deleted []string
	remove := func(p string) error {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		deleted = append(deleted, p)
		return nil
	}
	if _, err := os.Stat(m.ConfigPath); err == nil {
		if err := remove(m.ConfigPath); err != nil {
			return deleted, err
		}
	}
	if tmps, _ := filepath.Glob(m.ConfigPath + ".tmp-*"); len(tmps) > 0 {
		for _, p := range tmps {
			if err := remove(p); err != nil {
				return deleted, err
			}
		}
	}
	files := m.jsonResponseFiles()
	for _, p := range files {
		if err := remove(p); err != nil {
			return deleted, err
		}
	}
	if len(files) > 0 {
		_ = os.Remove(m.responsesDir()) // only succeeds when empty
	}
	return deleted, nil
}
