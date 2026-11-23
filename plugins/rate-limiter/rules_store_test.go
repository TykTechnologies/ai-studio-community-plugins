package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// studioStart is the plugin as AI Studio starts it: the rule store is the
// plugin's KV, counters is the configured backend's store, and the rules in
// the stored configuration are what it was started with. Initialize does
// the same wiring and then reconciles.
func studioStart(t *testing.T, kv *memKV, counters Store, storedConfig *string) *RateLimiterPlugin {
	t.Helper()
	var cfg struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal([]byte(*storedConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	p := newTestPlugin(nil)
	p.pluginID = 2
	p.store = counters
	p.rulesStore = newKVStore(kv)
	p.config.Rules = cfg.Rules
	// A counter store that is not the plugin's KV stands in for Redis, as
	// Initialize records when it opens one. The setting alone says nothing:
	// "redis" without a redis_url counts in the plugin's KV.
	if strings.Contains(*storedConfig, `"storage_backend":"redis"`) {
		p.config.StorageBackend = "redis"
	}
	_, isKV := counters.(*kvStore)
	p.countersSeparate = !isKV
	if len(cfg.Rules) > 0 {
		p.rulesCache = &RuleSet{Rules: cfg.Rules}
		p.rulesTTL = time.Now().Add(30 * time.Second)
	}
	p.reconcileRules()
	return p
}

func listRules(t *testing.T, p *RateLimiterPlugin) []Rule {
	t.Helper()
	raw, err := p.HandleRPC("listRules", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out RulesListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != len(out.Rules) {
		t.Fatalf("count %d but %d rules", out.Count, len(out.Rules))
	}
	return out.Rules
}

func createRule(t *testing.T, p *RateLimiterPlugin, name string) {
	t.Helper()
	payload := fmt.Sprintf(`{"name":%q,"dimensions":["app_id"],"limit":{"type":"requests","value":5}}`, name)
	if _, err := p.HandleRPC("createRule", []byte(payload)); err != nil {
		t.Fatalf("createRule %s: %v", name, err)
	}
}

func ruleNames(rules []Rule) string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.Name
	}
	return strings.Join(names, ",")
}

// storedRules are the rules in the plugin's stored configuration: what a
// push sends to the gateways.
func storedRules(t *testing.T, storedConfig *string) []Rule {
	t.Helper()
	var cfg struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal([]byte(*storedConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Rules
}

// Rules created on the kv backend are still there after a switch to Redis,
// can be edited, and a rule created then is added to them; the same going
// back. The rules were read from the counter store: after the switch the
// page showed none, updateRule said "rule not found", and the next rule
// created replaced the configuration's rules, so gateways lost the others.
func TestRules_SurviveABackendSwitchInBothDirections(t *testing.T) {
	kv := newMemKV()
	stored := stubConfigSync(t, `{"storage_backend":"kv"}`, nil)

	// On the kv backend the counters are in the plugin's KV too.
	onKV := studioStart(t, kv, newKVStore(kv), stored)
	for _, name := range []string{"one", "two", "three"} {
		createRule(t, onKV, name)
	}
	if got := ruleNames(storedRules(t, stored)); got != "one,two,three" {
		t.Fatalf("stored configuration rules = %q", got)
	}

	// Switch to Redis: a new start, with an empty counter store elsewhere.
	*stored = strings.Replace(*stored, `"storage_backend":"kv"`, `"storage_backend":"redis"`, 1)
	onRedis := studioStart(t, kv, newMemStore(), stored)
	rules := listRules(t, onRedis)
	if got := ruleNames(rules); got != "one,two,three" {
		t.Fatalf("Rules page after the switch to redis shows %q, want one,two,three", got)
	}

	// Editing an existing rule works.
	edit := rules[1]
	payload, _ := json.Marshal(UpdateRuleRequest{
		ID: edit.ID, Name: "two-edited", Dimensions: edit.Dimensions, Limit: edit.Limit, Action: edit.Action, Enabled: true,
	})
	if _, err := onRedis.HandleRPC("updateRule", payload); err != nil {
		t.Fatalf("updateRule after the switch: %v", err)
	}

	// Creating a fourth leaves four, on the page and in the configuration.
	createRule(t, onRedis, "four")
	if got := ruleNames(listRules(t, onRedis)); got != "one,two-edited,three,four" {
		t.Errorf("Rules page = %q", got)
	}
	if got := ruleNames(storedRules(t, stored)); got != "one,two-edited,three,four" {
		t.Errorf("stored configuration rules = %q: a rule write must never leave a partial set", got)
	}
	if !strings.Contains(*stored, `"storage_backend":"redis"`) {
		t.Errorf("the settings must be kept: %s", *stored)
	}

	// And back to kv.
	*stored = strings.Replace(*stored, `"storage_backend":"redis"`, `"storage_backend":"kv"`, 1)
	backOnKV := studioStart(t, kv, newKVStore(kv), stored)
	if got := ruleNames(listRules(t, backOnKV)); got != "one,two-edited,three,four" {
		t.Errorf("Rules page after the switch back to kv shows %q", got)
	}
	createRule(t, backOnKV, "five")
	if got := ruleNames(storedRules(t, stored)); got != "one,two-edited,three,four,five" {
		t.Errorf("stored configuration rules = %q", got)
	}
	// What the gateways enforce is what the page shows.
	if got, want := ruleNames(SortedRules(backOnKV.loadRulesCached())), ruleNames(listRules(t, backOnKV)); got != want {
		t.Errorf("enforced %q, page shows %q", got, want)
	}
}

// An upgrade from 1.2.1 that ran on Redis: its rules are in Redis and in the
// stored configuration, and the plugin's KV has an older set or none. The
// page shows the configuration's rules, which are the ones gateways enforce.
func TestRules_UpgradeFromRulesKeptInRedis(t *testing.T) {
	for name, kvRules := range map[string][]Rule{
		"KV has no rules":     nil,
		"KV has an older set": {{ID: "old", Name: "older", Dimensions: []string{"global"}, Limit: Limit{Type: "requests", Value: 1}, Action: "enforce", Enabled: true}},
		"KV already has them": {{ID: "a", Name: "a", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 5}, Action: "enforce", Enabled: true}},
	} {
		t.Run(name, func(t *testing.T) {
			current := []Rule{{ID: "a", Name: "a", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 5}, Action: "enforce", Enabled: true}}
			configJSON, _ := json.Marshal(map[string]interface{}{"storage_backend": "redis", "rules": current})
			stored := stubConfigSync(t, string(configJSON), nil)
			before := *stored

			kv := newMemKV()
			if kvRules != nil {
				blob, _ := json.Marshal(RuleSet{Rules: kvRules, Version: 3})
				kv.Write(context.Background(), rulesKVKey, blob, nil)
			}
			redis := newMemStore()
			blob, _ := json.Marshal(RuleSet{Rules: current, Version: 7})
			redis.Set(context.Background(), rulesKVKey, blob, 0)

			p := studioStart(t, kv, redis, stored)
			if got := ruleNames(listRules(t, p)); got != "a" {
				t.Fatalf("Rules page shows %q, want a", got)
			}
			if *stored != before {
				t.Errorf("reconciling must not rewrite a configuration that already has the rules: %s", *stored)
			}

			payload, _ := json.Marshal(UpdateRuleRequest{ID: "a", Name: "a2", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 9}, Action: "enforce", Enabled: true})
			if _, err := p.HandleRPC("updateRule", payload); err != nil {
				t.Fatalf("updateRule on an existing rule: %v", err)
			}
			createRule(t, p, "b")
			if got := ruleNames(storedRules(t, stored)); got != "a2,b" {
				t.Errorf("stored configuration rules = %q, want a2,b", got)
			}
		})
	}
}

// A configuration saved without its rules gets them back from the rule
// store at the next start.
func TestRules_ConfigurationWithoutRulesIsRefilled(t *testing.T) {
	kv := newMemKV()
	stored := stubConfigSync(t, `{"storage_backend":"kv"}`, nil)
	first := studioStart(t, kv, newKVStore(kv), stored)
	createRule(t, first, "one")
	createRule(t, first, "two")

	*stored = `{"storage_backend":"redis","fail_open":false}` // saved without "rules"
	p := studioStart(t, kv, newMemStore(), stored)
	if got := ruleNames(storedRules(t, stored)); got != "one,two" {
		t.Errorf("stored configuration rules = %q, want one,two", got)
	}
	if got := ruleNames(listRules(t, p)); got != "one,two" {
		t.Errorf("Rules page shows %q", got)
	}
	if !strings.Contains(*stored, `"fail_open":false`) {
		t.Errorf("the settings must be kept: %s", *stored)
	}
}

// Rules a 1.2.x plugin kept only in Redis (nothing in the configuration,
// nothing in the plugin's KV) are moved, not lost.
func TestRules_MovedFromTheCounterStore(t *testing.T) {
	stored := stubConfigSync(t, `{"storage_backend":"redis"}`, nil)
	redis := newMemStore()
	blob, _ := json.Marshal(RuleSet{Rules: []Rule{{ID: "r", Name: "in-redis", Dimensions: []string{"global"}, Limit: Limit{Type: "requests", Value: 3}, Action: "enforce", Enabled: true}}, Version: 2})
	redis.Set(context.Background(), rulesKVKey, blob, 0)

	p := studioStart(t, newMemKV(), redis, stored)
	if got := ruleNames(listRules(t, p)); got != "in-redis" {
		t.Errorf("Rules page shows %q", got)
	}
	if got := ruleNames(storedRules(t, stored)); got != "in-redis" {
		t.Errorf("stored configuration rules = %q", got)
	}
}

// A rule store that cannot be read is not written over, and neither is the
// configuration.
func TestRules_UnreadableRuleStoreIsLeftAlone(t *testing.T) {
	kv := newMemKV()
	kv.Write(context.Background(), rulesKVKey, []byte(`{"rules":[{"id":"keep","name":"keep"}],"version":4}`), nil)
	kv.readErr = errKVUnavailable
	stored := stubConfigSync(t, `{"storage_backend":"kv","rules":[{"id":"x","name":"x"}]}`, nil)
	before := *stored

	studioStart(t, kv, newMemStore(), stored)

	if *stored != before {
		t.Errorf("stored configuration changed: %s", *stored)
	}
	if got := string(kv.data[rulesKVKey]); !strings.Contains(got, `"keep"`) {
		t.Errorf("rule store was written over: %s", got)
	}
}

func deleteAllRules(t *testing.T, p *RateLimiterPlugin) {
	t.Helper()
	for _, r := range listRules(t, p) {
		payload, _ := json.Marshal(DeleteRuleRequest{ID: r.ID})
		if _, err := p.HandleRPC("deleteRule", payload); err != nil {
			t.Fatalf("deleteRule %s: %v", r.Name, err)
		}
	}
}

// The rule set 1.2.x kept in Redis is removed once the rules are in the
// rule store. Left there, it came back after an administrator deleted every
// rule: with the rule store and the configuration both empty, the next start
// read the old Redis copy and wrote it to both, and gateways enforced
// deleted rules after the next push.
func TestRules_DeletedRulesDoNotComeBackFromRedis(t *testing.T) {
	old := []Rule{{ID: "old", Name: "old-one", Dimensions: []string{"global"}, Limit: Limit{Type: "requests", Value: 3}, Action: "enforce", Enabled: true}}
	oldBlob, _ := json.Marshal(RuleSet{Rules: old, Version: 2})

	for name, configJSON := range map[string]string{
		"rules in Redis and in the configuration": `{"storage_backend":"redis","rules":[{"id":"old","name":"old-one","dimensions":["global"],"limit":{"type":"requests","value":3},"action":"enforce","enabled":true}]}`,
		"rules only in Redis":                     `{"storage_backend":"redis"}`,
	} {
		t.Run(name, func(t *testing.T) {
			stored := stubConfigSync(t, configJSON, nil)
			kv := newMemKV()
			redis := newMemStore()
			redis.Set(context.Background(), rulesKVKey, oldBlob, 0)

			// First start after the upgrade.
			p := studioStart(t, kv, redis, stored)
			if got := ruleNames(listRules(t, p)); got != "old-one" {
				t.Fatalf("after the upgrade the page shows %q, want old-one", got)
			}
			if _, err := redis.Get(context.Background(), rulesKVKey); err == nil {
				t.Error("the rule set must be gone from Redis once the rule store has it")
			}

			// The administrator deletes every rule.
			deleteAllRules(t, p)
			if got := ruleNames(storedRules(t, stored)); got != "" {
				t.Fatalf("stored configuration rules after deleting all = %q", got)
			}

			// Restart.
			again := studioStart(t, kv, redis, stored)
			if got := ruleNames(listRules(t, again)); got != "" {
				t.Errorf("after a restart the page shows %q: deleted rules came back", got)
			}
			if got := ruleNames(storedRules(t, stored)); got != "" {
				t.Errorf("after a restart the configuration has %q: gateways would enforce deleted rules", got)
			}
		})
	}
}

// On the kv backend the counter store is the plugin's KV: nothing is
// deleted there, the rules stay.
func TestRules_KVBackendKeepsItsRules(t *testing.T) {
	kv := newMemKV()
	stored := stubConfigSync(t, `{"storage_backend":"kv"}`, nil)
	p := studioStart(t, kv, newKVStore(kv), stored)
	createRule(t, p, "one")

	again := studioStart(t, kv, newKVStore(kv), stored)
	if got := ruleNames(listRules(t, again)); got != "one" {
		t.Errorf("Rules page shows %q, want one", got)
	}
	if _, ok := kv.data[rulesKVKey]; !ok {
		t.Error("the rule set was deleted from the plugin's KV")
	}
}

// storage_backend "redis" with no redis_url (or a Redis that could not be
// reached at start) counts in the plugin's KV. The rule store's own rules
// must not be taken for a copy left in Redis and deleted: the page then
// showed none, and the next rule created dropped the others from the
// configuration.
func TestRules_RedisSettingWithCountersInKVKeepsItsRules(t *testing.T) {
	kv := newMemKV()
	stored := stubConfigSync(t, `{"storage_backend":"redis"}`, nil)

	p := studioStart(t, kv, newKVStore(kv), stored)
	if p.config.StorageBackend != "redis" || p.countersSeparate {
		t.Fatalf("fixture: want the redis setting with counters in the plugin's KV")
	}
	createRule(t, p, "a")
	createRule(t, p, "b")

	for start := 1; start <= 2; start++ {
		again := studioStart(t, kv, newKVStore(kv), stored)
		if got := ruleNames(listRules(t, again)); got != "a,b" {
			t.Fatalf("start %d: Rules page shows %q, want a,b", start, got)
		}
		if _, ok := kv.data[rulesKVKey]; !ok {
			t.Fatalf("start %d: the rule set was deleted from the plugin's KV", start)
		}
	}

	last := studioStart(t, kv, newKVStore(kv), stored)
	createRule(t, last, "c")
	if got := ruleNames(storedRules(t, stored)); got != "a,b,c" {
		t.Errorf("stored configuration rules = %q, want a,b,c", got)
	}
}
