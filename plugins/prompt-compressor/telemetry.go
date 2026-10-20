package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// telemetry moves savings records from the edge to Studio. On the edge an
// Aggregator collects completed requests and a loop sends them as batches
// with SendToControlJSON. In Studio, AcceptEdgePayload merges the batches
// into hourly rollups in KV (records.Store).
type telemetry struct {
	agg   *records.Aggregator
	store *records.Store

	flushEvery time.Duration
	checkEvery time.Duration

	startOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	priceMu sync.Mutex
	prices  map[string]cachedPrice
}

type cachedPrice struct {
	price   records.Price
	ok      bool
	expires time.Time
}

const (
	telemetryFlushEvery = time.Minute
	telemetryCheckEvery = 5 * time.Second
	priceTTL            = 10 * time.Minute
	sendTimeout         = 10 * time.Second
)

func newTelemetry() *telemetry {
	ctx, cancel := context.WithCancel(context.Background())
	return &telemetry{
		flushEvery: telemetryFlushEvery,
		checkEvery: telemetryCheckEvery,
		ctx:        ctx,
		cancel:     cancel,
		prices:     map[string]cachedPrice{},
	}
}

// initTelemetry prepares the runtime's side: on an edge it points the
// response phase's recorder (SetRecorder) at the aggregator. It is safe to
// call on every Initialize.
func (p *PromptCompressorPlugin) initTelemetry(ctx plugin_sdk.Context) {
	t := p.telemetry
	switch ctx.Runtime {
	case plugin_sdk.RuntimeGateway:
		if t.agg == nil {
			t.agg = records.NewAggregator(p.lookupPrice)
			p.SetRecorder(t.agg)
		}
	case plugin_sdk.RuntimeStudio:
		if t.store == nil && ctx.Services != nil && ctx.Services.KV() != nil {
			t.store = records.NewStore(ctx.Services.KV(), isKVNotFound)
		}
	}
}

// startTelemetry starts the edge's send loop once the broker is up.
func (p *PromptCompressorPlugin) startTelemetry() {
	t := p.telemetry
	if t.agg == nil {
		return
	}
	t.startOnce.Do(func() {
		if t.ctx.Err() != nil {
			return
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			p.sendLoop(t)
		}()
	})
}

// stopTelemetry stops the send loop and sends what is left. It is safe to
// call more than once.
func (p *PromptCompressorPlugin) stopTelemetry() {
	t := p.telemetry
	t.cancel()
	t.wg.Wait()
	if t.agg != nil {
		p.flushRecords()
	}
}

func (p *PromptCompressorPlugin) sendLoop(t *telemetry) {
	flush := time.NewTicker(t.flushEvery)
	check := time.NewTicker(t.checkEvery)
	defer flush.Stop()
	defer check.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-flush.C:
			p.flushRecords()
		case <-check.C:
			// A full aggregator drops new keys: flush it early
			if t.agg.Full() {
				p.flushRecords()
			}
		}
	}
}

// flushRecords sends every pending batch. A batch the edge cannot queue
// goes back into the aggregator for the next flush.
func (p *PromptCompressorPlugin) flushRecords() {
	t := p.telemetry
	if dropped := t.agg.Dropped(); dropped > 0 {
		p.logger().Warn("Savings records dropped", "count", dropped)
	}
	batches := t.agg.Flush()
	if len(batches) == 0 {
		return
	}
	gw := p.gatewayServices()
	for i, b := range batches {
		if gw == nil {
			t.agg.Restore(b)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		_, err := gw.SendToControlJSON(ctx, b, b.ID, map[string]string{"metric_type": records.MetricType})
		cancel()
		if err != nil {
			p.logger().Warn("Could not queue savings records for Studio; keeping them for the next flush", "error", err.Error())
			for _, rest := range batches[i:] {
				t.agg.Restore(rest)
			}
			return
		}
	}
}

func (p *PromptCompressorPlugin) gatewayServices() plugin_sdk.GatewayServices {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.runtime != plugin_sdk.RuntimeGateway || p.services == nil {
		return nil
	}
	return p.services.Gateway()
}

// lookupPrice returns a model's input price from the edge's price list,
// cached for priceTTL (misses too).
func (p *PromptCompressorPlugin) lookupPrice(vendor, model string) (records.Price, bool) {
	t := p.telemetry
	key := vendor + "\x00" + model
	now := time.Now()
	t.priceMu.Lock()
	if c, ok := t.prices[key]; ok && now.Before(c.expires) {
		t.priceMu.Unlock()
		return c.price, c.ok
	}
	t.priceMu.Unlock()

	c := cachedPrice{expires: now.Add(priceTTL)}
	if gw := p.gatewayServices(); gw != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := gw.GetModelPrice(ctx, model, vendor)
		cancel()
		if err == nil {
			if r, ok := resp.(*mgmtpb.GetModelPriceResponse); ok && r.GetModelPrice().GetCpit() > 0 {
				mp := r.GetModelPrice()
				c.price = records.Price{Input: mp.GetCpit(), CacheRead: mp.GetCacheReadPt(), Currency: mp.GetCurrency()}
				if c.price.Currency == "" {
					c.price.Currency = "USD"
				}
				c.ok = true
			}
		}
	}
	t.priceMu.Lock()
	if len(t.prices) > 10000 {
		t.prices = map[string]cachedPrice{}
	}
	t.prices[key] = c
	t.priceMu.Unlock()
	return c.price, c.ok
}

// AcceptEdgePayload implements plugin_sdk.EdgePayloadReceiver: it merges an
// edge's savings records into Studio's rollups. A malformed batch is
// accepted and dropped (logged): resending it would never succeed. A KV
// failure is returned, so the payload counts as failed.
func (p *PromptCompressorPlugin) AcceptEdgePayload(ctx plugin_sdk.Context, payload *plugin_sdk.EdgePayload) (bool, error) {
	if payload == nil || payload.Metadata["metric_type"] != records.MetricType {
		return false, nil
	}
	store := p.telemetry.store
	if store == nil {
		return true, errNoStore
	}
	var b records.Batch
	if err := json.Unmarshal(payload.Payload, &b); err != nil {
		p.logger().Warn("Dropping malformed savings batch", "edge", records.EdgeKey(payload.EdgeNamespace, payload.EdgeID))
		return true, nil
	}
	if err := b.Validate(); err != nil {
		p.logger().Warn("Dropping invalid savings batch", "edge", records.EdgeKey(payload.EdgeNamespace, payload.EdgeID), "error", err.Error())
		return true, nil
	}
	kvCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.Apply(kvCtx, payload.EdgeNamespace, payload.EdgeID, &b); err != nil {
		return true, err
	}
	return true, nil
}

type telemetryError string

func (e telemetryError) Error() string { return string(e) }

const errNoStore = telemetryError("savings store unavailable: Studio KV is not ready")

// isKVNotFound recognises a missing key: Studio's KV Read answers gRPC
// NotFound.
func isKVNotFound(err error) bool {
	if status.Code(err) == codes.NotFound {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}
