package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

type SyncResult struct {
	StartedAt     time.Time `json:"started_at"`
	Duration      string    `json:"duration"`
	ModelsCreated int       `json:"models_created"`
	ModelsUpdated int       `json:"models_updated"`
	ModelsSkipped int       `json:"models_skipped"`
	Errors        []string  `json:"errors,omitempty"`
}

const perMillionTokens = 1_000_000.0

// syncPrices runs the sync using a scheduler-provided context for KV access.
func (p *PriceSyncPlugin) syncPrices(ctx plugin_sdk.Context) *SyncResult {
	return p.doSync(ctx.Services.KV())
}

// syncPricesFromRPC runs the sync using the stored KV reference (for RPC calls).
func (p *PriceSyncPlugin) syncPricesFromRPC() *SyncResult {
	return p.doSync(p.kv)
}

func (p *PriceSyncPlugin) doSync(kv plugin_sdk.KVService) *SyncResult {
	start := time.Now()
	result := &SyncResult{StartedAt: start}

	// Always persist result to KV, even on failure
	defer func() {
		result.Duration = time.Since(start).String()
		log.Printf("[price-sync] sync complete: created=%d updated=%d skipped=%d errors=%d duration=%s",
			result.ModelsCreated, result.ModelsUpdated, result.ModelsSkipped, len(result.Errors), result.Duration)
		storeSyncResult(kv, result)
	}()

	log.Printf("[price-sync] fetching prices from %s", p.config.PricesURL)
	prices, err := fetchCurrentPrices(p.config.PricesURL)
	if err != nil {
		log.Printf("[price-sync] fetch failed: %v", err)
		result.Errors = append(result.Errors, fmt.Sprintf("fetch failed: %v", err))
		return result
	}

	log.Printf("[price-sync] fetched %d model prices from source", len(prices))

	log.Printf("[price-sync] loading existing model prices from database")
	existing, err := loadExistingPrices()
	if err != nil {
		log.Printf("[price-sync] failed to load existing prices: %v", err)
		result.Errors = append(result.Errors, fmt.Sprintf("loading existing prices: %v", err))
		return result
	}
	log.Printf("[price-sync] loaded %d existing model prices", len(existing))
	log.Printf("[price-sync] vendor filter: %v (len=%d)", p.config.VendorFilter, len(p.config.VendorFilter))

	for _, ep := range prices {
		vendorSlug, ok := mapVendor(ep.Vendor)
		if !ok {
			result.ModelsSkipped++
			continue
		}

		// Filter: skip vendors not in the whitelist
		if len(p.config.VendorFilter) > 0 {
			slugMatch := containsVendor(p.config.VendorFilter, vendorSlug)
			rawMatch := containsVendor(p.config.VendorFilter, strings.ToLower(ep.Vendor))
			if !slugMatch && !rawMatch {
				result.ModelsSkipped++
				continue
			}
		}

		// Use the upstream ID as the model name (e.g. "claude-sonnet-4-6")
		modelName := ep.ID
		if modelName == "" {
			result.ModelsSkipped++
			continue
		}

		// Convert $/MTok to $/token
		cpit := safeDiv(ep.Input)
		cpt := safeDiv(ep.Output)
		cacheReadPT := safeDiv(ep.InputCached)
		cacheWritePT := 0.0 // not provided by source

		if ex, found := existing[modelName]; found {
			// Update if prices changed or vendor needs correcting
			vendorChanged := ex.vendor != vendorSlug
			pricesChanged := !pricesEqual(ex.cpt, cpt) || !pricesEqual(ex.cpit, cpit) ||
				!pricesEqual(ex.cacheReadPT, cacheReadPT) || !pricesEqual(ex.cacheWritePT, cacheWritePT)

			if !vendorChanged && !pricesChanged {
				result.ModelsSkipped++
				continue
			}

			if p.config.DryRun {
				log.Printf("[price-sync] [dry-run] would update %s (vendor=%s, vendor_changed=%v)", modelName, vendorSlug, vendorChanged)
				result.ModelsUpdated++
				continue
			}

			_, err := ai_studio_sdk.UpdateModelPrice(context.Background(), ex.id, modelName, vendorSlug, "USD", cpt, cpit, cacheWritePT, cacheReadPT)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %s: %v", modelName, err))
				continue
			}
			result.ModelsUpdated++
		} else {
			if !p.config.AutoCreateModels {
				result.ModelsSkipped++
				continue
			}

			if p.config.DryRun {
				log.Printf("[price-sync] [dry-run] would create %s (vendor=%s)", modelName, vendorSlug)
				result.ModelsCreated++
				continue
			}

			_, err := ai_studio_sdk.CreateModelPrice(context.Background(), modelName, vendorSlug, "USD", cpt, cpit, cacheWritePT, cacheReadPT)
			if err != nil {
				// Handle duplicate key: model already exists (race or stale cache)
				if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "UNIQUE constraint") {
					result.ModelsSkipped++
					continue
				}
				result.Errors = append(result.Errors, fmt.Sprintf("create %s: %v", modelName, err))
				continue
			}
			// Track newly created entry so duplicates within the same batch are caught
			existing[modelName] = existingPrice{vendor: vendorSlug, cpt: cpt, cpit: cpit, cacheReadPT: cacheReadPT, cacheWritePT: cacheWritePT}
			result.ModelsCreated++
		}
	}

	return result
}

type existingPrice struct {
	id           uint32
	vendor       string
	cpt          float64
	cpit         float64
	cacheReadPT  float64
	cacheWritePT float64
}

// loadExistingPrices returns a map keyed by model_name (not vendor:model_name)
// since model IDs from the source are globally unique. This allows the sync to
// detect and correct vendor mismatches from earlier runs.
func loadExistingPrices() (map[string]existingPrice, error) {
	result := make(map[string]existingPrice)

	page := int32(1)
	limit := int32(100)
	for {
		resp, err := ai_studio_sdk.ListModelPrices(context.Background(), "", page, limit)
		if err != nil {
			return nil, fmt.Errorf("listing model prices page %d: %w", page, err)
		}

		for _, mp := range resp.ModelPrices {
			result[mp.ModelName] = existingPrice{
				id:           mp.Id,
				vendor:       mp.Vendor,
				cpt:          mp.Cpt,
				cpit:         mp.Cpit,
				cacheReadPT:  mp.CacheReadPt,
				cacheWritePT: mp.CacheWritePt,
			}
		}

		if int32(len(resp.ModelPrices)) < limit {
			break
		}
		page++
	}

	return result, nil
}

func storeSyncResult(kv plugin_sdk.KVService, result *SyncResult) {
	ctx := context.Background()
	data, err := json.Marshal(result)
	if err != nil {
		log.Printf("[price-sync] failed to marshal sync result: %v", err)
		return
	}

	_, err = kv.Write(ctx, "price-sync:last-result", data, nil)
	if err != nil {
		log.Printf("[price-sync] failed to store sync result: %v", err)
	}

	// Append to history (keep last 10)
	appendToHistory(kv, data)
}

func appendToHistory(kv plugin_sdk.KVService, entry []byte) {
	ctx := context.Background()
	histData, _ := kv.Read(ctx, "price-sync:history")

	var history []json.RawMessage
	if len(histData) > 0 {
		json.Unmarshal(histData, &history)
	}

	history = append([]json.RawMessage{json.RawMessage(entry)}, history...)
	if len(history) > 10 {
		history = history[:10]
	}

	data, _ := json.Marshal(history)
	kv.Write(ctx, "price-sync:history", data, nil)
}

func safeDiv(val *float64) float64 {
	if val == nil {
		return 0
	}
	return *val / perMillionTokens
}

func pricesEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-15
}

func containsVendor(filter []string, vendor string) bool {
	for _, v := range filter {
		if v == vendor {
			return true
		}
	}
	return false
}
