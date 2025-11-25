package storage

import (
	"context"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/google/uuid"
)

const secretKeyPrefix = "github-rag:secret:"

// Secret represents stored authentication credentials
type Secret struct {
	ID             string `json:"id"`
	Type           string `json:"type"` // pat, ssh
	PATToken       string `json:"pat_token,omitempty"`
	SSHPrivateKey  string `json:"ssh_private_key,omitempty"`
	SSHPassphrase  string `json:"ssh_passphrase,omitempty"`
}

// SecretStore manages secret persistence
type SecretStore struct {
	kv *KVStore
}

// NewSecretStore creates a new secret store
func NewSecretStore(kv *KVStore) *SecretStore {
	return &SecretStore{kv: kv}
}

// Create creates a new secret
func (s *SecretStore) Create(ctx context.Context, secret *Secret) (string, error) {
	// Generate ID
	if secret.ID == "" {
		secret.ID = uuid.New().String()
	}

	// Store secret
	key := secretKeyPrefix + secret.ID
	if err := s.kv.Write(ctx, key, secret, nil); err != nil {
		return "", fmt.Errorf("failed to write secret: %w", err)
	}

	return secret.ID, nil
}

// Get retrieves a secret by ID
func (s *SecretStore) Get(ctx context.Context, id string) (*Secret, error) {
	var secret Secret
	key := secretKeyPrefix + id

	if err := s.kv.Read(ctx, key, &secret); err != nil {
		return nil, types.ErrSecretNotFound
	}

	return &secret, nil
}

// Delete deletes a secret
func (s *SecretStore) Delete(ctx context.Context, id string) error {
	key := secretKeyPrefix + id
	return s.kv.Delete(ctx, key)
}

// GetByRef retrieves a secret by reference (e.g., "secret:uuid")
func (s *SecretStore) GetByRef(ctx context.Context, ref string) (*Secret, error) {
	// Extract ID from reference format "secret:uuid"
	if len(ref) < 8 || ref[:7] != "secret:" {
		return nil, fmt.Errorf("invalid secret reference format: %s", ref)
	}

	id := ref[7:]
	return s.Get(ctx, id)
}
