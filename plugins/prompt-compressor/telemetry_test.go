package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// telemetryGateway is an edge: it queues what the plugin sends to control
// and answers price lookups.
type telemetryGateway struct {
	plugin_sdk.GatewayServices
	mu       sync.Mutex
	sent     []sentPayload
	failSend bool
	prices   map[string]*mgmtpb.ModelPriceInfo
}

type sentPayload struct {
	body        []byte
	correlation string
	metadata    map[string]string
}

func (g *telemetryGateway) SendToControlJSON(ctx context.Context, v interface{}, correlationID string, metadata map[string]string) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failSend {
		return 0, errors.New("queue unavailable")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	g.sent = append(g.sent, sentPayload{body: b, correlation: correlationID, metadata: metadata})
	return int64(len(g.sent)), nil
}

func (g *telemetryGateway) GetModelPrice(ctx context.Context, modelName, vendor string) (interface{}, error) {
	if p, ok := g.prices[vendor+"/"+modelName]; ok {
		return &mgmtpb.GetModelPriceResponse{ModelPrice: p}, nil
	}
	return nil, status.Error(codes.NotFound, "no price")
}

func (g *telemetryGateway) payloads() []sentPayload {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]sentPayload(nil), g.sent...)
}

// memKV is Studio's plugin KV: a missing key reads as gRPC NotFound.
type memKV struct {
	mu       sync.Mutex
	data     map[string][]byte
	failRead bool
}

func (m *memKV) Read(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failRead {
		return nil, status.Error(codes.Unavailable, "kv down")
	}
	v, ok := m.data[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "key not found")
	}
	return v, nil
}

func (m *memKV) Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	m.data[key] = append([]byte(nil), value...)
	return !ok, nil
}

func (m *memKV) WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return m.Write(ctx, key, value, nil)
}

func (m *memKV) Delete(ctx context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	delete(m.data, key)
	return ok, nil
}

func (m *memKV) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, errors.New("list operation not yet supported")
}

func (m *memKV) writes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.data)
}

type telemetryBroker struct {
	plugin_sdk.ServiceBroker
	gateway *telemetryGateway
	kv      *memKV
}

func (b *telemetryBroker) Gateway() plugin_sdk.GatewayServices {
	if b.gateway == nil {
		return nil
	}
	return b.gateway
}
func (b *telemetryBroker) KV() plugin_sdk.KVService {
	if b.kv == nil {
		return nil
	}
	return b.kv
}
func (b *telemetryBroker) Logger() plugin_sdk.LogService { return nil }

const policyHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func savingsRecord(app uint32, before, after int, outcome string) records.Request {
	return records.Request{
		At: time.Now(), AppID: app, LLMID: 3, Vendor: "anthropic", Model: "claude-x",
		PolicyName: "agents", PolicyHash: policyHash, Mode: "enforce", Level: 1, Outcome: outcome,
		Strategies:   map[string]records.StrategyStats{"json_compact": {Leaves: 1, BytesSaved: 10}},
		TokensBefore: before, TokensAfter: after, CompressMicros: 400,
		InputTokens: 5000, CacheReadTokens: 2000,
	}
}

func edgePlugin(t *testing.T, gw *telemetryGateway) *PromptCompressorPlugin {
	t.Helper()
	p := NewPromptCompressorPlugin()
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, Services: &telemetryBroker{gateway: gw}, Context: context.Background()}
	if err := p.Initialize(ctx, map[string]string{"_plugin_id": "1"}); err != nil {
		t.Fatal(err)
	}
	return p
}

func studioPlugin(t *testing.T, kv *memKV) *PromptCompressorPlugin {
	t.Helper()
	p := NewPromptCompressorPlugin()
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio, Services: &telemetryBroker{kv: kv}, Context: context.Background()}
	if err := p.Initialize(ctx, map[string]string{"plugin_id": "1"}); err != nil {
		t.Fatal(err)
	}
	return p
}

func deliver(t *testing.T, studio *PromptCompressorPlugin, edge string, sent []sentPayload) {
	t.Helper()
	for _, s := range sent {
		handled, err := studio.AcceptEdgePayload(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}, &plugin_sdk.EdgePayload{
			Payload: s.body, EdgeID: edge, CorrelationID: s.correlation, Metadata: s.metadata,
		})
		if !handled || err != nil {
			t.Fatalf("AcceptEdgePayload = %v, %v", handled, err)
		}
	}
}

// The data-layer check of eac.19: Studio's rollup totals equal the sum of
// what the edge recorded (the X-Tyk-Compression-Tokens-Saved values),
// through redelivery and across two edges.
func TestSavingsReachStudioRollups(t *testing.T) {
	gw1 := &telemetryGateway{prices: map[string]*mgmtpb.ModelPriceInfo{
		"anthropic/claude-x": {Cpit: 0.000003, CacheReadPt: 0.0000003, Currency: "USD"},
	}}
	gw2 := &telemetryGateway{}
	edge1, edge2 := edgePlugin(t, gw1), edgePlugin(t, gw2)
	kv := &memKV{data: map[string][]byte{}}
	studio := studioPlugin(t, kv)

	var wantSaved int64
	for i := 0; i < 40; i++ {
		r := savingsRecord(uint32(i%3+1), 1000+i, 600, records.OutcomeApplied)
		edge1.recorder().Add(r)
		wantSaved += int64(400 + i)
		edge2.recorder().Add(savingsRecord(9, 300, 300, records.OutcomeSkipped))
	}
	edge1.flushRecords()
	edge2.flushRecords()
	sent1, sent2 := gw1.payloads(), gw2.payloads()
	if len(sent1) == 0 || sent1[0].metadata["metric_type"] != records.MetricType {
		t.Fatalf("edge 1 sent %d payloads, metadata %v", len(sent1), sent1)
	}
	for _, s := range sent1 {
		if strings.Contains(string(s.body), "claude-x") {
			t.Error("the model name left the edge")
		}
	}
	deliver(t, studio, "edge-1", sent1)
	deliver(t, studio, "edge-2", sent2)
	deliver(t, studio, "edge-1", sent1) // redelivered after a hub restart

	now := records.HourOf(time.Now())
	rows, _, err := studio.telemetry.store.Query(context.Background(), now-1, now+1)
	if err != nil {
		t.Fatal(err)
	}
	var requests, saved int64
	var cost float64
	for _, d := range rows {
		requests += d.Requests
		saved += d.TokensSaved
		cost += d.CostSaved["USD"]
	}
	if requests != 80 || saved != wantSaved {
		t.Errorf("Studio totals %d requests, %d tokens saved; edges recorded 80 and %d", requests, saved, wantSaved)
	}
	if want := float64(wantSaved) * 0.000003; cost < want*0.999999 || cost > want*1.000001 {
		t.Errorf("cost saved %v, want %v", cost, want)
	}
}

func TestUnsentBatchesAreKeptForTheNextFlush(t *testing.T) {
	gw := &telemetryGateway{failSend: true}
	p := edgePlugin(t, gw)
	p.recorder().Add(savingsRecord(1, 100, 50, records.OutcomeApplied))
	p.flushRecords()
	if len(gw.payloads()) != 0 {
		t.Fatal("sent while the queue failed")
	}
	gw.mu.Lock()
	gw.failSend = false
	gw.mu.Unlock()
	p.recorder().Add(savingsRecord(1, 100, 50, records.OutcomeApplied))
	p.flushRecords()
	sent := gw.payloads()
	if len(sent) != 1 {
		t.Fatalf("%d payloads", len(sent))
	}
	var b records.Batch
	_ = json.Unmarshal(sent[0].body, &b)
	if b.Deltas[0].Requests != 2 {
		t.Errorf("%d requests sent, want both", b.Deltas[0].Requests)
	}
}

func TestShutdownSendsWhatIsLeft(t *testing.T) {
	gw := &telemetryGateway{}
	p := edgePlugin(t, gw)
	p.telemetry.flushEvery = time.Hour
	p.OnSessionReady(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway})
	p.recorder().Add(savingsRecord(1, 100, 50, records.OutcomeApplied))

	done := make(chan struct{})
	go func() {
		_ = p.Shutdown(plugin_sdk.Context{})
		_ = p.Shutdown(plugin_sdk.Context{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return: the send loop ignored the stop")
	}
	if len(gw.payloads()) != 1 {
		t.Errorf("%d payloads after Shutdown, want the last records sent", len(gw.payloads()))
	}
}

func TestSendLoopFlushesOnItsTicker(t *testing.T) {
	gw := &telemetryGateway{}
	p := edgePlugin(t, gw)
	p.telemetry.flushEvery = 20 * time.Millisecond
	p.OnSessionReady(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway})
	p.OnSessionReady(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}) // starts once
	defer p.Shutdown(plugin_sdk.Context{})
	p.recorder().Add(savingsRecord(1, 100, 50, records.OutcomeApplied))
	deadline := time.Now().Add(3 * time.Second)
	for len(gw.payloads()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(gw.payloads()) == 0 {
		t.Error("the send loop never flushed")
	}
}

func TestAcceptEdgePayloadRefusals(t *testing.T) {
	kv := &memKV{data: map[string][]byte{}}
	studio := studioPlugin(t, kv)
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}

	// Someone else's payload
	if handled, err := studio.AcceptEdgePayload(ctx, &plugin_sdk.EdgePayload{Payload: []byte(`{}`), Metadata: map[string]string{"metric_type": "llm_cache_stats"}}); handled || err != nil {
		t.Errorf("another plugin's payload: %v, %v", handled, err)
	}
	// Malformed and invalid batches are taken and dropped, never retried
	meta := map[string]string{"metric_type": records.MetricType}
	for _, body := range []string{`not json`, `{"v":1,"id":"zz","deltas":[]}`, `{"v":9,"id":"0123456789abcdef0123456789abcdef","deltas":[]}`} {
		handled, err := studio.AcceptEdgePayload(ctx, &plugin_sdk.EdgePayload{Payload: []byte(body), EdgeID: "e", Metadata: meta})
		if !handled || err != nil {
			t.Errorf("%s: %v, %v; want taken and dropped", body, handled, err)
		}
	}
	if kv.writes() != 0 {
		t.Errorf("%d KV keys written for bad batches", kv.writes())
	}
	// A KV failure is reported, so the payload counts as failed
	agg := records.NewAggregator(nil)
	agg.Add(savingsRecord(1, 100, 50, records.OutcomeApplied))
	body, _ := json.Marshal(agg.Flush()[0])
	kv.mu.Lock()
	kv.failRead = true
	kv.mu.Unlock()
	if _, err := studio.AcceptEdgePayload(ctx, &plugin_sdk.EdgePayload{Payload: body, EdgeID: "e", Metadata: meta}); err == nil {
		t.Error("a KV failure was swallowed")
	}
	// A gateway plugin has no store
	edge := edgePlugin(t, &telemetryGateway{})
	if _, err := edge.AcceptEdgePayload(ctx, &plugin_sdk.EdgePayload{Payload: body, Metadata: meta}); err == nil {
		t.Error("an edge accepted savings records")
	}
	_ = fmt.Sprint()
}

func TestPriceLookupIsCached(t *testing.T) {
	gw := &telemetryGateway{prices: map[string]*mgmtpb.ModelPriceInfo{
		"openai/gpt-x": {Cpit: 0.000002, Currency: ""},
	}}
	p := edgePlugin(t, gw)
	pr, ok := p.lookupPrice("openai", "gpt-x")
	if !ok || pr.Input != 0.000002 || pr.Currency != "USD" || pr.CacheRead != 0 {
		t.Errorf("price = %+v, %v", pr, ok)
	}
	if _, ok := p.lookupPrice("openai", "unknown"); ok {
		t.Error("an unknown model was priced")
	}
	delete(gw.prices, "openai/gpt-x")
	if _, ok := p.lookupPrice("openai", "gpt-x"); !ok {
		t.Error("the price was not cached")
	}
}
