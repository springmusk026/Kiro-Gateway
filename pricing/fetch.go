package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the models.dev catalogue.
const DefaultURL = "https://models.dev/api.json"

const (
	refreshEvery = 24 * time.Hour
	retryAfter   = time.Hour
	cacheFile    = "pricing-cache.json"
	maxBody      = 64 << 20
)

// Enabled reports whether list prices are fetched (KIRO_PRICING=off turns it
// off; KIRO_PRICING_URL points at another copy of api.json).
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("KIRO_PRICING")))
	return v != "off" && v != "0" && v != "false"
}

func sourceURL() string {
	if u := strings.TrimSpace(os.Getenv("KIRO_PRICING_URL")); u != "" {
		return u
	}
	return DefaultURL
}

// Fetch downloads and parses the catalogue.
func Fetch(ctx context.Context, client *http.Client, url string) (*Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kiro-gateway-pricing")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return ParseModelsDev(data, url)
}

// LoadCache reads the trimmed catalogue saved in dir.
func LoadCache(dir string) (*Catalog, error) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFile))
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if len(c.Models) == 0 {
		return nil, fmt.Errorf("empty pricing cache")
	}
	return &c, nil
}

// SaveCache writes the trimmed catalogue to dir (a few dozen KB).
func SaveCache(dir string, c *Catalog) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, cacheFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, cacheFile))
}

// Logger is the subset of the gateway logger the refresher uses.
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

// Run loads the cached catalogue from dir, then keeps it fresh until ctx ends:
// it refetches when the copy is a day old, and retries hourly after a failure.
// onUpdate (optional) runs after every catalogue it installs.
func Run(ctx context.Context, dir string, client *http.Client, log Logger, onUpdate func()) {
	if !Enabled() {
		return
	}
	if c, err := LoadCache(dir); err == nil {
		Set(c)
		if onUpdate != nil {
			onUpdate()
		}
	}
	for {
		wait := time.Duration(0)
		if c := Current(); c != nil {
			wait = time.Until(c.FetchedAt.Add(refreshEvery))
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		c, err := Fetch(fctx, client, sourceURL())
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warnf("[Pricing] Could not update list prices from %s: %v (retrying in %s)", sourceURL(), err, retryAfter)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryAfter):
			}
			continue
		}
		Set(c)
		if err := SaveCache(dir, c); err != nil {
			log.Warnf("[Pricing] Could not save the price cache: %v", err)
		}
		log.Infof("[Pricing] Loaded list prices for %d models from models.dev", len(c.Models))
		if onUpdate != nil {
			onUpdate()
		}
	}
}
