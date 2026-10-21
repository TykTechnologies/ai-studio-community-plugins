package config

import "testing"

func TestAppPolicyFromMetadata(t *testing.T) {
	for metadata, want := range map[string]string{
		``:                                    "",
		`not json`:                            "",
		`{}`:                                  "",
		`{"prompt_compressor.policy": "max"}`: "max",
		`{"prompt_compressor": {"policy": "off"}}`: "off",
		`{"prompt_compressor.policy": 3}`:          "",
		`{"prompt_compressor": "max"}`:             "",
		`{"other": 1, "prompt_compressor.policy": "safe", "prompt_compressor": {"policy": "max"}}`: "safe",
	} {
		if got := AppPolicyFromMetadata(metadata); got != want {
			t.Errorf("AppPolicyFromMetadata(%s) = %q, want %q", metadata, got, want)
		}
	}
}

// An App metadata override is resolved end to end; a value that names no
// policy falls back to the bindings.
func TestAppMetadataOverride(t *testing.T) {
	cfg := mustParse(t, `{"policies": {"max": {"level": 3}}, "bindings": {"llm:claude": "max"}}`)
	for _, tc := range []struct {
		metadata   string
		wantPolicy string
		wantSource Source
	}{
		{`{"prompt_compressor.policy": "off"}`, "off", SourceAppMetadata},
		{`{"prompt_compressor.policy": "no-such-policy"}`, "max", SourceLLMBinding},
		{`{}`, "max", SourceLLMBinding},
	} {
		r := cfg.Resolve(Target{AppID: 1, LLMSlug: "claude"}, AppPolicyFromMetadata(tc.metadata))
		if r.Name != tc.wantPolicy || r.Source != tc.wantSource {
			t.Errorf("%s: %s from %s, want %s from %s", tc.metadata, r.Name, r.Source, tc.wantPolicy, tc.wantSource)
		}
	}
}
