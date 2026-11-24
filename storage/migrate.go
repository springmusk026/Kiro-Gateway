package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kiro-go/config"
)

// ImportResult summarizes a JSON → database import.
type ImportResult struct {
	Accounts  int
	APIKeys   int
	Responses int
	Skipped   int
}

// ErrDatabaseNotEmpty prevents an import from overwriting existing data.
var ErrDatabaseNotEmpty = errors.New("database already contains configuration (use --force to overwrite)")

// ImportJSON copies config.json (and the sibling responses/ directory of
// stored /v1/responses objects) into the database. The JSON files are left in
// place as a backup. It refuses to run against a non-empty database unless
// force is set, in which case existing configuration rows are replaced.
func ImportJSON(ctx context.Context, db *DB, configPath string, force bool) (*ImportResult, error) {
	c, err := config.ReadJSONFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	if c == nil {
		return nil, fmt.Errorf("%s does not exist", configPath)
	}
	cs := NewConfigStore(db)
	empty, err := cs.Empty(ctx)
	if err != nil {
		return nil, err
	}
	if !empty {
		if !force {
			return nil, ErrDatabaseNotEmpty
		}
		if _, err := db.Pool.Exec(ctx, `TRUNCATE kiro_settings, kiro_accounts, kiro_api_keys`); err != nil {
			return nil, err
		}
	}
	if err := cs.Save(c); err != nil {
		return nil, fmt.Errorf("write configuration: %w", err)
	}
	res := &ImportResult{Accounts: len(c.Accounts), APIKeys: len(c.ApiKeys)}

	// Stored responses: data/responses/<id>.json next to config.json.
	dir := filepath.Join(filepath.Dir(configPath), "responses")
	entries, err := os.ReadDir(dir)
	if err == nil {
		rs := NewResponseStore(db)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			full := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(full)
			if err != nil || !json.Valid(data) {
				res.Skipped++
				continue
			}
			var meta struct {
				ID       string `json:"id"`
				StoredAt int64  `json:"stored_at"`
			}
			_ = json.Unmarshal(data, &meta)
			id := meta.ID
			if id == "" {
				id = strings.TrimSuffix(e.Name(), ".json")
			}
			at := time.Unix(meta.StoredAt, 0)
			if meta.StoredAt == 0 {
				if info, err := e.Info(); err == nil {
					at = info.ModTime()
				} else {
					at = time.Now()
				}
			}
			if err := rs.SaveResponse(ctx, id, data, at); err != nil {
				res.Skipped++
				continue
			}
			res.Responses++
		}
	}
	return res, nil
}

// ExportJSON writes the database configuration to a config.json file (the
// rollback path back to the JSON backend). By default encrypted credentials
// are copied as they are stored (and remaining plaintext is encrypted when
// KIRO_ENCRYPTION_KEY is set); decrypt writes every credential in plaintext
// and needs the key.
func ExportJSON(ctx context.Context, db *DB, outPath string, decrypt bool) (*config.Config, error) {
	cs := NewConfigStore(db)
	out := &config.JSONStore{Path: outPath}
	if decrypt {
		out.Codec = config.PlaintextSecretCodec()
	} else {
		cs.Codec = config.RawSecretCodec()
	}
	c, err := cs.Load()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("database contains no configuration")
	}
	if err := out.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}
