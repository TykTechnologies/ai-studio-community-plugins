package config

import (
	"strings"
	"testing"
)

// eac.63: a top-level value can be a $SECRET/ reference the host resolved.
// Errors about it name the setting and the type it needs, never the value,
// because config errors are shown to every plugin reader.
func TestErrorsNeverRepeatTopLevelValues(t *testing.T) {
	const canary = "sk-CANARY-91f2e0"
	for _, key := range []string{
		"mode", "default_policy", "failure_mode", "allow_client_bypass", "lossy_with_request_filters",
		"max_body_bytes", "cache_drop_alert", "policies", "bindings",
	} {
		_, err := Parse(map[string]string{key: canary})
		if err == nil {
			t.Errorf("%s: a resolved secret was accepted as a value", key)
			continue
		}
		if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "CANARY") {
			t.Errorf("%s: the error repeats the value: %v", key, err)
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("%s: the error does not name the setting: %v", key, err)
		}
	}
}
