package secrets

import (
	"strings"
	"testing"
)

func TestResolveConfigRef(t *testing.T) {
	t.Setenv("RAG_TEST_VAULT_TOKEN", "hvs.from-env")

	got, err := ResolveConfigRef("vault_token", "hvs.literal")
	if err != nil || got != "hvs.literal" {
		t.Errorf("literal: %q, %v", got, err)
	}

	got, err = ResolveConfigRef("vault_token", "$ENV/RAG_TEST_VAULT_TOKEN")
	if err != nil || got != "hvs.from-env" {
		t.Errorf("$ENV reference: %q, %v", got, err)
	}

	if _, err := ResolveConfigRef("vault_token", "$ENV/RAG_TEST_UNSET_VAR"); err == nil || !strings.Contains(err.Error(), "PLUGIN_ENV_PASSTHROUGH") {
		t.Errorf("unset $ENV reference: %v", err)
	}

	if _, err := ResolveConfigRef("vault_token", "$SECRET/VAULT_TOKEN"); err == nil || !strings.Contains(err.Error(), "not resolved") {
		t.Errorf("unresolved $SECRET reference: %v", err)
	}

	// Only a leading reference counts
	got, err = ResolveConfigRef("vault_token", "token-with-$SECRET/inside")
	if err != nil || got != "token-with-$SECRET/inside" {
		t.Errorf("mid-string reference: %q, %v", got, err)
	}
}
