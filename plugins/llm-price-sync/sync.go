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

// syncPrices runs the sync under the scheduler's context and KV service.
func (p *PriceSyncPlugin) syncPrices(ctx plugin_sdk.Context) (*SyncResult, error) {
	return p.doSync(ctx.Context, ctx.Services.KV())
}

// syncPricesFromRPC runs the sync using the stored KV reference (for RPC calls).
func (p *PriceSyncPlugin) syncPricesFromRPC(ctx context.Context) (*SyncResult, error) {
	return p.doSync(ctx, p.kv)
}

// doSync fetches prices and applies them. Cancelling ctx stops the sync
// between models. The result is always persisted; a failure to persist it is
// the returned error.
func (p *PriceSyncPlugin) doSync(ctx context.Context, kv plugin_sdk.KVService) (result *SyncResult, persistErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	result = &SyncResult{StartedAt: start}

	// Always persist result to KV, even on failure or cancellation
	defer func() {
		result.Duration = time.Since(start).String()
		log.Printf("[price-sync] sync complete: created=%d updated=%d skipped=%d errors=%d duration=%s",
			result.ModelsCreated, result.ModelsUpdated, result.ModelsSkipped, len(result.Errors), result.Duration)
		persistErr = p.storeSyncResult(context.WithoutCancel(ctx), kv, result)
	}()

	log.Printf("[price-sync] fetching prices from %s", p.config.PricesURL)
	prices, err := fetchCurrentPrices(ctx, p.config.PricesURL)
	if err != nil {
		log.Printf("[price-sync] fetch failed: %v", err)
		result.Errors = append(result.Errors, fmt.Sprintf("fetch failed: %v", err))
		return result, nil
	}

	log.Printf("[price-sync] fetched %d model prices from source", len(prices))

	log.Printf("[price-sync] loading existing model prices from database")
	existing, err := loadExistingPrices(ctx)
	if err != nil {
		log.Printf("[price-sync] failed to load existing prices: %v", err)
		result.Errors = append(result.Errors, fmt.Sprintf("loading existing prices: %v", err))
		return result, nil
	}
	log.Printf("[price-sync] loaded %d existing model prices", len(existing))
	log.Printf("[price-sync] vendor filter: %v (len=%d)", p.config.VendorFilter, len(p.config.VendorFilter))

	for _, ep := range prices {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("sync stopped: %v", err))
			return result, nil
		}
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

			_, err := ai_studio_sdk.UpdateModelPrice(ctx, ex.id, modelName, vendorSlug, "USD", cpt, cpit, cacheWritePT, cacheReadPT)
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

			_, err := ai_studio_sdk.CreateModelPrice(ctx, modelName, vendorSlug, "USD", cpt, cpit, cacheWritePT, cacheReadPT)
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

	return result, nil
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
func loadExistingPrices(ctx context.Context) (map[string]existingPrice, error) {
	result := make(map[string]existingPrice)

	page := int32(1)
	limit := int32(100)
	for {
		resp, err := ai_studio_sdk.ListModelPrices(ctx, "", page, limit)
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

// KV keys for sync results; the history keeps the last historyLen, newest first.
const (
	lastResultKey = "price-sync:last-result"
	historyKey    = "price-sync:history"
	historyLen    = 10
)

// storeSyncResult persists result as the last result and in the history.
func (p *PriceSyncPlugin) storeSyncResult(ctx context.Context, kv plugin_sdk.KVService, result *SyncResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal sync result: %w", err)
	}
	if _, err := kv.Write(ctx, lastResultKey, data, nil); err != nil {
		log.Printf("[price-sync] failed to store sync result: %v", err)
		return fmt.Errorf("store sync result: %w", err)
	}
	return p.appendToHistory(ctx, kv, data)
}

// appendToHistory prepends entry to the history. The read-modify-write runs
// under historyMu, so a scheduled sync and a manual one cannot drop each
// other's entries.
func (p *PriceSyncPlugin) appendToHistory(ctx context.Context, kv plugin_sdk.KVService, entry []byte) error {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()

	var history []json.RawMessage
	if histData, err := kv.Read(ctx, historyKey); err == nil && len(histData) > 0 {
		if err := json.Unmarshal(histData, &history); err != nil {
			// Start a fresh history rather than failing every sync on it
			log.Printf("[price-sync] discarding unreadable sync history: %v", err)
			history = nil
		}
	}

	history = append([]json.RawMessage{json.RawMessage(entry)}, history...)
	if len(history) > historyLen {
		history = history[:historyLen]
	}

	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("marshal sync history: %w", err)
	}
	if _, err := kv.Write(ctx, historyKey, data, nil); err != nil {
		log.Printf("[price-sync] failed to store sync history: %v", err)
		return fmt.Errorf("store sync history: %w", err)
	}
	return nil
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
