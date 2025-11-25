package secrets

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	vault "github.com/hashicorp/vault/api"
)

// VaultBackend implements secret storage using HashiCorp Vault
type VaultBackend struct {
	client     *vault.Client
	mountPath  string
	secretPath string
}

// NewVaultBackend creates a new Vault-based secrets backend
func NewVaultBackend(config *Config) (*VaultBackend, error) {
	// Create Vault client
	vaultConfig := vault.DefaultConfig()
	vaultConfig.Address = config.VaultAddress

	client, err := vault.NewClient(vaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	// Set token
	client.SetToken(config.VaultToken)

	return &VaultBackend{
		client:     client,
		mountPath:  config.VaultMountPath,
		secretPath: config.VaultSecretPath,
	}, nil
}

// Store stores a secret in Vault and returns its reference
func (b *VaultBackend) Store(ctx context.Context, secret *storage.Secret) (string, error) {
	// Generate ID for the secret if not set
	if secret.ID == "" {
		secret.ID = generateSecretID()
	}

	// Build Vault path
	vaultPath := path.Join(b.mountPath, "data", b.secretPath, secret.ID)

	// Prepare secret data
	data := map[string]interface{}{
		"data": map[string]interface{}{
			"type":            secret.Type,
			"pat_token":       secret.PATToken,
			"ssh_private_key": secret.SSHPrivateKey,
			"ssh_passphrase":  secret.SSHPassphrase,
		},
	}

	// Write to Vault
	_, err := b.client.Logical().Write(vaultPath, data)
	if err != nil {
		return "", fmt.Errorf("failed to write secret to Vault: %w", err)
	}

	// Return reference in format "vault:secret_id"
	return fmt.Sprintf("vault:%s", secret.ID), nil
}

// Retrieve retrieves a secret from Vault by reference
func (b *VaultBackend) Retrieve(ctx context.Context, ref string) (*storage.Secret, error) {
	// Extract secret ID from reference "vault:secret_id"
	if len(ref) < 7 || ref[:6] != "vault:" {
		return nil, fmt.Errorf("invalid vault reference format: %s", ref)
	}

	secretID := ref[6:]

	// Build Vault path
	vaultPath := path.Join(b.mountPath, "data", b.secretPath, secretID)

	// Read from Vault
	vaultSecret, err := b.client.Logical().Read(vaultPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret from Vault: %w", err)
	}

	if vaultSecret == nil || vaultSecret.Data == nil {
		return nil, fmt.Errorf("secret not found in Vault: %s", secretID)
	}

	// Extract data (Vault KV v2 wraps data in "data" key)
	secretData, ok := vaultSecret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid secret data format in Vault")
	}

	// Build Secret struct
	secret := &storage.Secret{
		ID:   secretID,
		Type: getStringField(secretData, "type"),
		PATToken: getStringField(secretData, "pat_token"),
		SSHPrivateKey: getStringField(secretData, "ssh_private_key"),
		SSHPassphrase: getStringField(secretData, "ssh_passphrase"),
	}

	return secret, nil
}

// Delete deletes a secret from Vault by reference
func (b *VaultBackend) Delete(ctx context.Context, ref string) error {
	// Extract secret ID
	if len(ref) < 7 || ref[:6] != "vault:" {
		return fmt.Errorf("invalid vault reference format: %s", ref)
	}

	secretID := ref[6:]

	// Build Vault path (use metadata path for deletion)
	vaultPath := path.Join(b.mountPath, "metadata", b.secretPath, secretID)

	// Delete from Vault
	_, err := b.client.Logical().Delete(vaultPath)
	if err != nil {
		return fmt.Errorf("failed to delete secret from Vault: %w", err)
	}

	return nil
}

// Helper functions

func getStringField(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func generateSecretID() string {
	// Use simple UUID generation
	// In production, consider using a more secure method
	return fmt.Sprintf("secret-%d", time.Now().UnixNano())
}
