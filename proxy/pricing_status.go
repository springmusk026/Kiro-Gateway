package proxy

import (
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/pricing"
)

// pricingInfo tells the console where list prices come from.
type pricingInfo struct {
	Enabled        bool      `json:"enabled"`
	Source         string    `json:"source,omitempty"`
	FetchedAt      time.Time `json:"fetchedAt,omitempty"`
	Models         int       `json:"models"`
	CreditPriceUSD float64   `json:"creditPriceUsd"`
}

func pricingStatus() pricingInfo {
	info := pricingInfo{Enabled: pricing.Enabled(), CreditPriceUSD: config.CreditPriceUSD()}
	if c := pricing.Current(); c != nil {
		info.Source, info.FetchedAt, info.Models = c.Source, c.FetchedAt, len(c.Models)
	}
	return info
}
