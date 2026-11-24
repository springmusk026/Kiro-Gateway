package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Store persists the gateway configuration (settings, accounts, API keys).
//
// The in-memory Config stays the source of truth while running; a Store only
// loads it at start-up and persists snapshots. Implementations decide how much
// to write (the Postgres store writes only changed rows).
type Store interface {
	// Load returns the stored config, or (nil, nil) when nothing is stored yet.
	Load() (*Config, error)
	// Save persists a snapshot. It is called with cfgLock held, so it must not
	// call back into this package's locking functions.
	Save(c *Config) error
	// Name describes the backend for logs ("json:/app/data/config.json", "postgres").
	Name() string
}

var (
	storeMu sync.RWMutex
	store   Store
)

// UseStore selects the persistence backend. Call before Load/Init.
func UseStore(s Store) {
	storeMu.Lock()
	store = s
	storeMu.Unlock()
}

// CurrentStore returns the active backend (the JSON file store by default).
func CurrentStore() Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if store != nil {
		return store
	}
	return &JSONStore{Path: cfgPath}
}

// JSONStore keeps the configuration in a single JSON file (the original format).
type JSONStore struct {
	Path string
}

func (s *JSONStore) Name() string { return "json:" + s.Path }

func (s *JSONStore) Load() (*Config, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *JSONStore) Save(c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Path, data, 0600)
}

// ReadJSONFile loads a config.json without touching global state (used by the
// JSON → database migrator).
func ReadJSONFile(path string) (*Config, error) {
	return (&JSONStore{Path: path}).Load()
}

// Snapshot returns a deep copy of the current configuration.
func Snapshot() (*Config, error) {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil, ErrConfigUnavailable
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Coalesced background saves for high-frequency, low-value changes (request
// counters, per-account and per-key usage). These used to persist on every
// request, putting storage I/O on the request path; now they are flushed at
// most once per saveDebounce, outside any request.
const saveDebounce = time.Second

var (
	pendingMu    sync.Mutex
	pendingTimer *time.Timer
)

// scheduleSaveLocked marks the config dirty. Caller holds cfgLock (it only
// arms a timer, so it never blocks on I/O).
func scheduleSaveLocked() {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if pendingTimer != nil {
		return
	}
	pendingTimer = time.AfterFunc(saveDebounce, flushPending)
}

func flushPending() {
	pendingMu.Lock()
	pendingTimer = nil
	pendingMu.Unlock()
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return
	}
	if err := Save(); err != nil {
		fmt.Fprintf(os.Stderr, "config: background save failed: %v\n", err)
	}
}

// Flush writes any pending coalesced changes now (call on shutdown).
func Flush() {
	pendingMu.Lock()
	t := pendingTimer
	pendingMu.Unlock()
	if t != nil && t.Stop() {
		flushPending()
	}
}

// MergeMissing adds the accounts and API keys from src that the current
// configuration does not have (matched by ID; API keys also by key value) and
// persists the result. Existing entries are never modified.
func MergeMissing(src *Config) (accounts, keys int, err error) {
	if src == nil {
		return 0, 0, nil
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return 0, 0, ErrConfigUnavailable
	}
	haveAcc := make(map[string]bool, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		haveAcc[a.ID] = true
	}
	for _, a := range src.Accounts {
		if a.ID == "" || haveAcc[a.ID] {
			continue
		}
		cfg.Accounts = append(cfg.Accounts, a)
		haveAcc[a.ID] = true
		accounts++
	}
	haveKey := make(map[string]bool, 2*len(cfg.ApiKeys))
	for _, k := range cfg.ApiKeys {
		haveKey["id:"+k.ID] = true
		haveKey["key:"+k.Key] = true
	}
	for _, k := range src.ApiKeys {
		if k.Key == "" || haveKey["id:"+k.ID] || haveKey["key:"+k.Key] {
			continue
		}
		cfg.ApiKeys = append(cfg.ApiKeys, k)
		haveKey["id:"+k.ID], haveKey["key:"+k.Key] = true, true
		keys++
	}
	if accounts == 0 && keys == 0 {
		return 0, 0, nil
	}
	return accounts, keys, Save()
}
