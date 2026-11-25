// Package pricing prices requests at public API list prices, from the
// models.dev catalogue (https://models.dev).
//
// Kiro bills in credits. The list price shows what the same tokens would have
// cost on the model vendor's own API, so the analytics can compare the two.
// Prices are per million tokens; the catalogue is fetched once a day and a
// trimmed copy is kept next to the configuration for offline restarts.
package pricing

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Price is a model's list price in USD per million tokens.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
	// Tiers apply when the prompt is larger than their threshold (for example
	// a higher price above 200k tokens of context), largest threshold last.
	Tiers []Tier `json:"tiers,omitempty"`
}

// Tier is the price for prompts larger than Above tokens.
type Tier struct {
	Above      int     `json:"above"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

// Model is one priced model.
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider"`
	Price    Price  `json:"price"`
}

// Catalog is an immutable set of priced models.
type Catalog struct {
	FetchedAt time.Time        `json:"fetchedAt"`
	Source    string           `json:"source"`
	Models    map[string]Model `json:"models"` // lowercased id -> model
}

// providers are the first-party vendors whose prices are used, in order of
// preference when two list the same id. Resellers and clouds are ignored.
var providers = []string{
	"anthropic", "openai", "zai", "google", "deepseek", "xai", "mistral",
	"moonshotai", "minimax", "alibaba",
}

type mdCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
	Tiers      []struct {
		Input      *float64 `json:"input"`
		Output     *float64 `json:"output"`
		CacheRead  *float64 `json:"cache_read"`
		CacheWrite *float64 `json:"cache_write"`
		Tier       struct {
			Type string `json:"type"`
			Size int    `json:"size"`
		} `json:"tier"`
	} `json:"tiers"`
}

type mdProvider struct {
	Models map[string]struct {
		ID   string  `json:"id"`
		Name string  `json:"name"`
		Cost *mdCost `json:"cost"`
	} `json:"models"`
}

func f(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// ParseModelsDev builds a catalogue from models.dev's api.json.
func ParseModelsDev(data []byte, source string) (*Catalog, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("models.dev: %w", err)
	}
	c := &Catalog{FetchedAt: time.Now().UTC(), Source: source, Models: map[string]Model{}}
	for _, prov := range providers {
		body, ok := raw[prov]
		if !ok {
			continue
		}
		var p mdProvider
		if err := json.Unmarshal(body, &p); err != nil {
			continue
		}
		for key, m := range p.Models {
			if m.Cost == nil || (m.Cost.Input == nil && m.Cost.Output == nil) {
				continue
			}
			id := strings.ToLower(strings.TrimSpace(m.ID))
			if id == "" {
				id = strings.ToLower(key)
			}
			if _, dup := c.Models[id]; dup {
				continue
			}
			price := Price{Input: f(m.Cost.Input), Output: f(m.Cost.Output), CacheRead: f(m.Cost.CacheRead), CacheWrite: f(m.Cost.CacheWrite)}
			for _, t := range m.Cost.Tiers {
				if t.Tier.Type != "context" || t.Tier.Size <= 0 {
					continue
				}
				price.Tiers = append(price.Tiers, Tier{Above: t.Tier.Size, Input: f(t.Input), Output: f(t.Output), CacheRead: f(t.CacheRead), CacheWrite: f(t.CacheWrite)})
			}
			sort.Slice(price.Tiers, func(i, j int) bool { return price.Tiers[i].Above < price.Tiers[j].Above })
			c.Models[id] = Model{ID: id, Name: m.Name, Provider: prov, Price: price}
		}
	}
	if len(c.Models) == 0 {
		return nil, fmt.Errorf("models.dev: no priced models found")
	}
	return c, nil
}

var current atomic.Pointer[Catalog]

// Set installs c as the active catalogue (nil clears it).
func Set(c *Catalog) { current.Store(c) }

// Current returns the active catalogue, or nil before one is loaded.
func Current() *Catalog { return current.Load() }

// candidates lists the ids a Kiro model may have in the catalogue:
// "claude-haiku-5.5" is "claude-haiku-5-5" there.
func candidates(model string) []string {
	id := strings.ToLower(strings.TrimSpace(model))
	id = strings.TrimSuffix(id, "-thinking")
	out := []string{id}
	if strings.HasPrefix(id, "claude-") && strings.Contains(id, ".") {
		out = append(out, strings.ReplaceAll(id, ".", "-"))
	}
	return out
}

// Lookup finds the list price of a Kiro model id.
func Lookup(model string) (Model, bool) {
	c := current.Load()
	if c == nil || model == "" {
		return Model{}, false
	}
	for _, id := range candidates(model) {
		if m, ok := c.Models[id]; ok {
			return m, true
		}
	}
	return Model{}, false
}

// Usage is the token usage of one request. Input is the whole prompt,
// including any cached part reported in CacheRead and CacheWrite.
type Usage struct {
	Input, Output, CacheRead, CacheWrite int
}

// Cost returns the list price in USD of one request on model, and false when
// the model has no known price.
func Cost(model string, u Usage) (float64, bool) {
	m, ok := Lookup(model)
	if !ok {
		return 0, false
	}
	return m.Price.Cost(u), true
}

// Cost prices one request.
func (p Price) Cost(u Usage) float64 {
	in, out, cr, cw := p.Input, p.Output, p.CacheRead, p.CacheWrite
	for _, t := range p.Tiers {
		if u.Input > t.Above {
			in, out, cr, cw = t.Input, t.Output, t.CacheRead, t.CacheWrite
		}
	}
	if cr == 0 {
		cr = in
	}
	if cw == 0 {
		cw = in
	}
	cached := u.CacheRead + u.CacheWrite
	uncached := u.Input - cached
	if uncached < 0 {
		uncached = 0
	}
	return (float64(uncached)*in + float64(u.Output)*out + float64(u.CacheRead)*cr + float64(u.CacheWrite)*cw) / 1e6
}
