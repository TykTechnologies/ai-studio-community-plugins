package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
)

// stubConfigSync replaces the Service API calls syncRulesToConfig makes with
// an in-memory stored config, returning a pointer to it.
func stubConfigSync(t *testing.T, stored string, getErr error) *string {
	t.Helper()
	oldGet, oldUpdate := getPluginForSync, updatePluginConfigForSync
	t.Cleanup(func() { getPluginForSync, updatePluginConfigForSync = oldGet, oldUpdate })
	getPluginForSync = func(ctx context.Context, id uint32) (*mgmtpb.GetPluginResponse, error) {
		if getErr != nil {
			return nil, getErr
		}
		return &mgmtpb.GetPluginResponse{Plugin: &mgmtpb.PluginInfo{Id: id, ConfigJson: stored}}, nil
	}
	updatePluginConfigForSync = func(ctx context.Context, id uint32, cfg string) (*mgmtpb.UpdatePluginConfigResponse, error) {
		stored = cfg
		return &mgmtpb.UpdatePluginConfigResponse{Success: true}, nil
	}
	return &stored
}

// A rule edit writes the rules into the stored config without reverting
// settings saved since the plugin started (lsj.24): the process still holds
// kv / fail_open true / 60s, the stored config says redis / false / 30s.
func TestSyncRulesToConfigKeepsStoredSettings(t *testing.T) {
	p := newTestPlugin(nil) // in memory: kv, fail_open true, window 60
	p.pluginID = 2
	stored := stubConfigSync(t, `{"storage_backend":"redis","redis_url":"redis://r:6379/5","redis_password":"$SECRET/REDIS","window_size_seconds":30,"fail_open":false,"rules":[]}`, nil)

	if _, err := p.rpcCreateRule([]byte(`{"name":"r1","dimensions":["app_id"],"limit":{"type":"requests","value":5},"action":"enforce"}`)); err != nil {
		t.Fatalf("createRule: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(*stored), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"storage_backend": "redis", "redis_url": "redis://r:6379/5", "redis_password": "$SECRET/REDIS",
		"window_size_seconds": float64(30), "fail_open": false,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	rules, _ := got["rules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("rules = %v, want the new rule", got["rules"])
	}
}

// If the stored config cannot be read, nothing is written: a stale copy
// would overwrite the stored settings.
func TestSyncRulesToConfigSkipsWhenStoredConfigUnreadable(t *testing.T) {
	p := newTestPlugin(nil)
	p.pluginID = 2
	before := `{"storage_backend":"redis","fail_open":false}`
	stored := stubConfigSync(t, before, errors.New("unavailable"))

	p.syncRulesToConfig([]Rule{{ID: "x", Name: "x"}})
	if *stored != before {
		t.Errorf("stored config changed to %s", *stored)
	}
}
