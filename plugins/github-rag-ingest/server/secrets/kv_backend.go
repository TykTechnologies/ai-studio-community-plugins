package secrets

import (
	"context"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
)

// KVBackend implements secret storage using the KV store
type KVBackend struct {
	store *storage.SecretStore
}

// NewKVBackend creates a new KV-based secrets backend
func NewKVBackend(secretStore *storage.SecretStore) *KVBackend {
	return &KVBackend{
		store: secretStore,
	}
}

// Store stores a secret in the KV store and returns its reference
func (b *KVBackend) Store(ctx context.Context, secret *storage.Secret) (string, error) {
	id, err := b.store.Create(ctx, secret)
	if err != nil {
		return "", err
	}

	// Return reference in format "secret:uuid"
	return fmt.Sprintf("secret:%s", id), nil
}

// Retrieve retrieves a secret by reference
func (b *KVBackend) Retrieve(ctx context.Context, ref string) (*storage.Secret, error) {
	return b.store.GetByRef(ctx, ref)
}

// Delete deletes a secret by reference
func (b *KVBackend) Delete(ctx context.Context, ref string) error {
	// Extract ID from reference
	if len(ref) < 8 || ref[:7] != "secret:" {
		return fmt.Errorf("invalid secret reference format: %s", ref)
	}

	id := ref[7:]
	return b.store.Delete(ctx, id)
}
