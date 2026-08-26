package storage

import (
	"context"
	"encoding/json"
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

// SecretStore manages secret persistence. When an encryption key is set
// (see NewEncryptedSecretStore), secrets are encrypted with AES-256-GCM
// before being written to KV; legacy plaintext records remain readable.
type SecretStore struct {
	kv  *KVStore
	key []byte // nil = plaintext (legacy) mode
}

// NewSecretStore creates a secret store without encryption at rest.
// Prefer NewEncryptedSecretStore; this exists for backward compatibility
// when no encryption key has been configured.
func NewSecretStore(kv *KVStore) *SecretStore {
	return &SecretStore{kv: kv}
}

// NewEncryptedSecretStore creates a secret store that encrypts secrets with
// AES-256-GCM using a key derived from the given passphrase. Plaintext
// records written before encryption was enabled are still readable.
func NewEncryptedSecretStore(kv *KVStore, passphrase string) (*SecretStore, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("secret encryption passphrase cannot be empty")
	}
	return &SecretStore{kv: kv, key: deriveSecretKey(passphrase)}, nil
}

// Create creates a new secret
func (s *SecretStore) Create(ctx context.Context, secret *Secret) (string, error) {
	// Generate ID
	if secret.ID == "" {
		secret.ID = uuid.New().String()
	}

	payload, err := json.Marshal(secret)
	if err != nil {
		return "", fmt.Errorf("failed to marshal secret: %w", err)
	}

	if s.key != nil {
		payload, err = encryptSecretPayload(s.key, payload)
		if err != nil {
			return "", fmt.Errorf("failed to encrypt secret: %w", err)
		}
	}

	// Store secret
	key := secretKeyPrefix + secret.ID
	if err := s.kv.WriteRaw(ctx, key, payload, nil); err != nil {
		return "", fmt.Errorf("failed to write secret: %w", err)
	}

	return secret.ID, nil
}

// Get retrieves a secret by ID
func (s *SecretStore) Get(ctx context.Context, id string) (*Secret, error) {
	key := secretKeyPrefix + id

	stored, err := s.kv.ReadRaw(ctx, key)
	if err != nil {
		return nil, types.ErrSecretNotFound
	}

	payload, _, err := decryptSecretPayload(s.key, stored)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret %s: %w", id, err)
	}

	var secret Secret
	if err := json.Unmarshal(payload, &secret); err != nil {
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
