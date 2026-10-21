package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// eac.76: policies:read may not see what an engine URL carries besides
// scheme, host and path, and saving the masked view back keeps the URL.
// (Userinfo is refused by the config check since eac.65; TestMaskEngineURL
// covers its masking for configs stored before that.)
func TestEditorMasksCredentialPartsOfEngineURLs(t *testing.T) {
	const canary = "CANARY-7f3a"
	studio := &fakeStudio{stored: `{"engine_h_url": "https://h.example:8443/v1?api_key=` + canary + `#` + canary + `", "engine_h_token": "t", "engine_r_url": "$SECRET/r"}`}
	p := editorPlugin(t, studio)

	raw, err := p.HandleRPC("getEditor", []byte(`{"_service_broker_id": 3}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), canary) {
		t.Fatalf("getEditor leaked an engine URL credential: %s", raw)
	}
	var view editorView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	want := "https://h.example:8443/v1?" + maskedToken + "#" + maskedToken
	if view.Config["engine_h_url"] != want || view.Config["engine_r_url"] != "$SECRET/r" {
		t.Errorf("engine URLs shown as %v and %v", view.Config["engine_h_url"], view.Config["engine_r_url"])
	}

	// The view saved back unchanged keeps the stored URL
	cfg := view.Config
	cfg["mode"] = "enforce"
	var res saveResult
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg, "_service_broker_id": 3}, &res); err != nil || !res.Saved {
		t.Fatalf("saveConfig = %+v, %v", res, err)
	}
	var after map[string]interface{}
	if err := json.Unmarshal([]byte(studio.stored), &after); err != nil {
		t.Fatal(err)
	}
	if u, _ := after["engine_h_url"].(string); !strings.Contains(u, "api_key="+canary) || after["mode"] != "enforce" {
		t.Errorf("stored after save: url %q, mode %v", u, after["mode"])
	}

	// Writing a URL is still refused, with or without the masked parts
	cfg["engine_h_url"] = "https://h.example:8443/v1"
	res = saveResult{}
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg, "_service_broker_id": 3}, &res); err != nil || res.Saved {
		t.Fatalf("a changed engine URL was saved: %+v, %v", res, err)
	}
	if strings.Contains(strings.Join(res.Errors, " "), canary) {
		t.Errorf("the refusal repeated the stored URL: %v", res.Errors)
	}
}

func TestMaskEngineURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787"},
		{"https://h.example/v1/compress", "https://h.example/v1/compress"},
		{"https://h.example/?key=s", "https://h.example/?" + maskedToken},
		{"https://h.example?", "https://h.example?" + maskedToken},
		{"https://a:b@h.example", "https://" + maskedToken + "@h.example"},
		{"https://h.example/#s", "https://h.example/#" + maskedToken},
		{"h.example/v1?key=s", maskedToken},
		{"mailto:s@h.example", maskedToken},
		{"%zz", maskedToken},
	} {
		if got := maskEngineURL(tc.in); got != tc.want {
			t.Errorf("maskEngineURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
