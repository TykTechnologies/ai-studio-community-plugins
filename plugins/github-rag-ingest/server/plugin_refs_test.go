package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type nopKV struct{}

func (nopKV) Read(context.Context, string) ([]byte, error) {
	return nil, status.Error(codes.NotFound, "key not found")
}
func (nopKV) Write(context.Context, string, []byte, *time.Time) (bool, error) { return true, nil }
func (nopKV) WriteWithTTL(context.Context, string, []byte, time.Duration) (bool, error) {
	return true, nil
}
func (nopKV) Delete(context.Context, string) (bool, error)   { return true, nil }
func (nopKV) List(context.Context, string) ([]string, error) { return nil, nil }

type nopLogger struct{}

func (nopLogger) Debug(string, ...interface{}) {}
func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Warn(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}

// testBroker provides the KV and logger Initialize uses.
type testBroker struct{ plugin_sdk.ServiceBroker }

func (testBroker) KV() plugin_sdk.KVService      { return nopKV{} }
func (testBroker) Logger() plugin_sdk.LogService { return nopLogger{} }

func initialize(t *testing.T, config map[string]string) error {
	config["cache_path"] = t.TempDir()
	return NewGitHubRAGPlugin().Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio, Services: testBroker{}}, config)
}

// An unresolved secret reference for the encryption key fails Initialize: it
// never falls back to GITHUB_RAG_SECRET_ENCRYPTION_KEY or plaintext storage.
func TestInitializeFailsOnUnresolvedEncryptionKeyReference(t *testing.T) {
	t.Setenv("GITHUB_RAG_SECRET_ENCRYPTION_KEY", "fallback-must-not-be-used")
	err := initialize(t, map[string]string{"secret_encryption_key": "$SECRET/MISSING"})
	if err == nil || !strings.Contains(err.Error(), "secret_encryption_key") {
		t.Fatalf("Initialize = %v, want an unresolved-reference error", err)
	}

	t.Setenv("RAG_TEST_ENC_KEY", "a-strong-passphrase")
	if err := initialize(t, map[string]string{"secret_encryption_key": "$ENV/RAG_TEST_ENC_KEY"}); err != nil {
		t.Fatalf("Initialize with an $ENV/ reference: %v", err)
	}
}
