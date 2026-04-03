package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// externalResponse is the top-level wrapper from llm-prices.com
type externalResponse struct {
	UpdatedAt string          `json:"updated_at"`
	Prices    []ExternalPrice `json:"prices"`
}

// ExternalPrice represents a single model pricing entry from llm-prices.com
// Prices are in $/MTok (per million tokens).
type ExternalPrice struct {
	ID          string   `json:"id"`
	Vendor      string   `json:"vendor"`
	Name        string   `json:"name"`
	Input       *float64 `json:"input"`
	Output      *float64 `json:"output"`
	InputCached *float64 `json:"input_cached"`
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func fetchCurrentPrices(url string) ([]ExternalPrice, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "TykAIStudio-PriceSyncPlugin/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching prices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var parsed externalResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing prices JSON: %w", err)
	}

	return parsed.Prices, nil
}
