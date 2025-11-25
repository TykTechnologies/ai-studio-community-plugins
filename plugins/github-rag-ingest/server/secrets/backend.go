package secrets

import (
	"context"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
)

// Backend defines the interface for secret storage backends
type Backend interface {
	// Store stores a secret and returns its reference
	Store(ctx context.Context, secret *storage.Secret) (string, error)

	// Retrieve retrieves a secret by reference
	Retrieve(ctx context.Context, ref string) (*storage.Secret, error)

	// Delete deletes a secret by reference
	Delete(ctx context.Context, ref string) error
}

// Config holds secrets backend configuration
type Config struct {
	Backend          string // "kv" or "vault"
	VaultAddress     string
	VaultToken       string
	VaultMountPath   string
	VaultSecretPath  string
}
