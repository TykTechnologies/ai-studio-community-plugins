package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fakeKV is an in-memory plugin_sdk.KVService for tests.
type fakeKV struct {
	data map[string][]byte
}

func newFakeKV() *fakeKV {
	return &fakeKV{data: map[string][]byte{}}
}

func (f *fakeKV) Read(ctx context.Context, key string) ([]byte, error) {
	v, ok := f.data[key]
	if !ok {
		return nil, contextKeyNotFoundErr{}
	}
	return v, nil
}

func (f *fakeKV) Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error) {
	_, existed := f.data[key]
	f.data[key] = value
	return !existed, nil
}

func (f *fakeKV) WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return f.Write(ctx, key, value, nil)
}

func (f *fakeKV) Delete(ctx context.Context, key string) (bool, error) {
	_, existed := f.data[key]
	delete(f.data, key)
	return existed, nil
}

func (f *fakeKV) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

type contextKeyNotFoundErr struct{}

func (contextKeyNotFoundErr) Error() string { return "key not found" }

func testSecret() *Secret {
	return &Secret{
		Type:          "ssh",
		PATToken:      "ghp_supersecrettoken",
		SSHPrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nsecretmaterial\n-----END OPENSSH PRIVATE KEY-----",
		SSHPassphrase: "hunter2",
	}
}

// TestSecretStore_EncryptsAtRest is the core regression test: with an
// encryption key configured, secret material must not appear in the raw KV
// bytes.
func TestSecretStore_EncryptsAtRest(t *testing.T) {
	kv := newFakeKV()
	store, err := NewEncryptedSecretStore(NewKVStore(kv), "test-passphrase")
	if err != nil {
		t.Fatalf("failed to create encrypted store: %v", err)
	}

	ctx := context.Background()
	id, err := store.Create(ctx, testSecret())
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	raw, ok := kv.data[secretKeyPrefix+id]
	if !ok {
		t.Fatal("secret was not written to KV")
	}

	for _, sensitive := range []string{"ghp_supersecrettoken", "secretmaterial", "hunter2"} {
		if strings.Contains(string(raw), sensitive) {
			t.Errorf("raw KV bytes contain plaintext secret material %q", sensitive)
		}
	}

	// Round trip: the store must decrypt what it wrote.
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	want := testSecret()
	if got.PATToken != want.PATToken || got.SSHPrivateKey != want.SSHPrivateKey || got.SSHPassphrase != want.SSHPassphrase {
		t.Error("decrypted secret does not match the original")
	}
}

// TestSecretStore_ReadsLegacyPlaintext ensures secrets stored before
// encryption was introduced are still readable.
func TestSecretStore_ReadsLegacyPlaintext(t *testing.T) {
	kv := newFakeKV()
	ctx := context.Background()

	legacy := testSecret()
	legacy.ID = "legacy-id"
	data, _ := json.Marshal(legacy)
	kv.data[secretKeyPrefix+"legacy-id"] = data

	store, err := NewEncryptedSecretStore(NewKVStore(kv), "test-passphrase")
	if err != nil {
		t.Fatalf("failed to create encrypted store: %v", err)
	}

	got, err := store.Get(ctx, "legacy-id")
	if err != nil {
		t.Fatalf("Get of legacy plaintext secret failed: %v", err)
	}
	if got.PATToken != legacy.PATToken {
		t.Error("legacy secret round trip mismatch")
	}
}

// TestSecretStore_WrongKeyFails ensures decryption with the wrong key errors
// out instead of returning garbage.
func TestSecretStore_WrongKeyFails(t *testing.T) {
	kv := newFakeKV()
	ctx := context.Background()

	store1, _ := NewEncryptedSecretStore(NewKVStore(kv), "correct-passphrase")
	id, err := store1.Create(ctx, testSecret())
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	store2, _ := NewEncryptedSecretStore(NewKVStore(kv), "wrong-passphrase")
	if _, err := store2.Get(ctx, id); err == nil {
		t.Error("expected decryption with wrong key to fail")
	}
}

// TestSecretStore_EmptyPassphraseRejected ensures the encrypted constructor
// refuses an empty passphrase.
func TestSecretStore_EmptyPassphraseRejected(t *testing.T) {
	if _, err := NewEncryptedSecretStore(NewKVStore(newFakeKV()), ""); err == nil {
		t.Error("expected empty passphrase to be rejected")
	}
}

// TestSecretStore_PlaintextStoreStillWorks covers the backward-compatible
// unencrypted store used when no key is configured.
func TestSecretStore_PlaintextStoreStillWorks(t *testing.T) {
	kv := newFakeKV()
	store := NewSecretStore(NewKVStore(kv))
	ctx := context.Background()

	id, err := store.Create(ctx, testSecret())
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.PATToken != testSecret().PATToken {
		t.Error("plaintext store round trip mismatch")
	}
}
