package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"kiro-go/config"
)

// ConfigStore persists the gateway configuration in three tables: one
// settings row (everything except accounts and API keys), one row per
// account and one row per API key. Save diffs against what was last written
// and touches only changed rows, so a stats update on one account is a single
// small UPDATE instead of rewriting the whole configuration.
//
// Credentials are encrypted per row with KIRO_ENCRYPTION_KEY (config.SecretCodec)
// when it is set; change detection hashes the plaintext, so re-encryption with
// a fresh nonce does not make every row look changed.
type ConfigStore struct {
	db *DB
	// Codec overrides the default secret codec (nil = KIRO_ENCRYPTION_KEY).
	Codec *config.SecretCodec

	mu       sync.Mutex
	settings [32]byte
	accounts map[string][32]byte
	keys     map[string][32]byte
}

func NewConfigStore(db *DB) *ConfigStore {
	return &ConfigStore{db: db, accounts: map[string][32]byte{}, keys: map[string][32]byte{}}
}

func (s *ConfigStore) Name() string { return "postgres" }

func (s *ConfigStore) codec() *config.SecretCodec {
	if s.Codec != nil {
		return s.Codec
	}
	return config.DefaultSecretCodec()
}

// Empty reports whether nothing has been stored yet (used for first-run import).
func (s *ConfigStore) Empty(ctx context.Context) (bool, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM kiro_settings`).Scan(&n)
	return n == 0, err
}

func (s *ConfigStore) Load() (*config.Config, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var settings []byte
	err := s.db.Pool.QueryRow(ctx, `SELECT data FROM kiro_settings WHERE id = 1`).Scan(&settings)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	var c config.Config
	if err := json.Unmarshal(settings, &c); err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}
	c.Accounts = []config.Account{}
	c.ApiKeys = nil
	codec := s.codec()
	encrypting := codec.Enabled()
	// Rows holding plaintext credentials while encryption is on are not
	// remembered as stored, so the next save rewrites them encrypted.
	stale := map[string]bool{}
	settingsPlain, err := codec.DecryptSettings(&c)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Pool.Query(ctx, `SELECT data FROM kiro_accounts ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var a config.Account
		if err := json.Unmarshal(raw, &a); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode account: %w", err)
		}
		plain, err := codec.DecryptAccount(&a)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if plain && encrypting {
			stale["a:"+a.ID] = true
		}
		c.Accounts = append(c.Accounts, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.Pool.Query(ctx, `SELECT data FROM kiro_api_keys ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("load api keys: %w", err)
	}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var k config.ApiKeyEntry
		if err := json.Unmarshal(raw, &k); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode api key: %w", err)
		}
		plain, err := codec.DecryptApiKey(&k)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if plain && encrypting {
			stale["k:"+k.ID] = true
		}
		c.ApiKeys = append(c.ApiKeys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Everything just loaded is, by definition, what is stored.
	s.mu.Lock()
	s.remember(&c)
	if settingsPlain && encrypting {
		s.settings = [32]byte{}
	}
	for id := range stale {
		if id[0] == 'a' {
			delete(s.accounts, id[2:])
		} else {
			delete(s.keys, id[2:])
		}
	}
	s.mu.Unlock()
	if (settingsPlain || len(stale) > 0) && encrypting {
		config.NotePlaintextSecrets()
	}
	return &c, nil
}

type rowWrite struct {
	id       string
	position int
	data     []byte
	email    string
	enabled  bool
}

func hashOf(position int, data []byte) [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "%d:", position)
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func settingsJSON(c *config.Config) ([]byte, error) {
	cp := *c
	cp.Accounts = nil
	cp.ApiKeys = nil
	return json.Marshal(&cp)
}

func (s *ConfigStore) remember(c *config.Config) {
	if data, err := settingsJSON(c); err == nil {
		s.settings = hashOf(0, data)
	}
	s.accounts = map[string][32]byte{}
	for i := range c.Accounts {
		if data, err := json.Marshal(&c.Accounts[i]); err == nil {
			s.accounts[c.Accounts[i].ID] = hashOf(i, data)
		}
	}
	s.keys = map[string][32]byte{}
	for i := range c.ApiKeys {
		if data, err := json.Marshal(&c.ApiKeys[i]); err == nil {
			s.keys[c.ApiKeys[i].ID] = hashOf(i, data)
		}
	}
}

// Save writes the rows that differ from the last successful write, in one
// transaction. Unchanged configurations cost no database round trip.
func (s *ConfigStore) Save(c *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	codec := s.codec()
	settings, err := settingsJSON(c)
	if err != nil {
		return err
	}
	settingsHash := hashOf(0, settings)
	settingsChanged := settingsHash != s.settings
	if settingsChanged && codec.Enabled() {
		enc := *c
		if err := codec.EncryptSettings(&enc); err != nil {
			return err
		}
		if settings, err = settingsJSON(&enc); err != nil {
			return err
		}
	}

	var accWrites []rowWrite
	accSeen := make(map[string]bool, len(c.Accounts))
	newAcc := make(map[string][32]byte, len(c.Accounts))
	for i := range c.Accounts {
		a := &c.Accounts[i]
		data, err := json.Marshal(a)
		if err != nil {
			return err
		}
		h := hashOf(i, data)
		accSeen[a.ID] = true
		newAcc[a.ID] = h
		if s.accounts[a.ID] != h {
			if codec.Enabled() {
				enc, err := codec.EncryptAccount(*a)
				if err != nil {
					return err
				}
				if data, err = json.Marshal(&enc); err != nil {
					return err
				}
			}
			accWrites = append(accWrites, rowWrite{id: a.ID, position: i, data: data, email: a.Email, enabled: a.Enabled})
		}
	}
	var accDeletes []string
	for id := range s.accounts {
		if !accSeen[id] {
			accDeletes = append(accDeletes, id)
		}
	}

	var keyWrites []rowWrite
	keySeen := make(map[string]bool, len(c.ApiKeys))
	newKeys := make(map[string][32]byte, len(c.ApiKeys))
	for i := range c.ApiKeys {
		k := &c.ApiKeys[i]
		data, err := json.Marshal(k)
		if err != nil {
			return err
		}
		h := hashOf(i, data)
		keySeen[k.ID] = true
		newKeys[k.ID] = h
		if s.keys[k.ID] != h {
			if codec.Enabled() {
				enc, err := codec.EncryptApiKey(*k)
				if err != nil {
					return err
				}
				if data, err = json.Marshal(&enc); err != nil {
					return err
				}
			}
			keyWrites = append(keyWrites, rowWrite{id: k.ID, position: i, data: data})
		}
	}
	var keyDeletes []string
	for id := range s.keys {
		if !keySeen[id] {
			keyDeletes = append(keyDeletes, id)
		}
	}

	if !settingsChanged && len(accWrites) == 0 && len(accDeletes) == 0 && len(keyWrites) == 0 && len(keyDeletes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b := &pgx.Batch{}
	if settingsChanged {
		b.Queue(`INSERT INTO kiro_settings (id, data, updated_at) VALUES (1, $1, now())
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()`, settings)
	}
	for _, w := range accWrites {
		b.Queue(`INSERT INTO kiro_accounts (id, position, email, enabled, data, updated_at) VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (id) DO UPDATE SET position = EXCLUDED.position, email = EXCLUDED.email,
			enabled = EXCLUDED.enabled, data = EXCLUDED.data, updated_at = now()`,
			w.id, w.position, w.email, w.enabled, w.data)
	}
	if len(accDeletes) > 0 {
		b.Queue(`DELETE FROM kiro_accounts WHERE id = ANY($1)`, accDeletes)
	}
	for _, w := range keyWrites {
		b.Queue(`INSERT INTO kiro_api_keys (id, position, data, updated_at) VALUES ($1, $2, $3, now())
			ON CONFLICT (id) DO UPDATE SET position = EXCLUDED.position, data = EXCLUDED.data, updated_at = now()`,
			w.id, w.position, w.data)
	}
	if len(keyDeletes) > 0 {
		b.Queue(`DELETE FROM kiro_api_keys WHERE id = ANY($1)`, keyDeletes)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		tx.Rollback(ctx)
		return fmt.Errorf("save config: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.settings = settingsHash
	s.accounts = newAcc
	s.keys = newKeys
	return nil
}
