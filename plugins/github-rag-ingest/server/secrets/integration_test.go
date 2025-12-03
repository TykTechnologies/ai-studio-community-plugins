//go:build integration

package secrets

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/pkg/testinfra/containers"
)

var sharedVaultContainer *containers.VaultContainer

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var exitCode int
	defer func() {
		if sharedVaultContainer != nil {
			log.Println("Stopping Vault container...")
			sharedVaultContainer.Close(context.Background())
		}
		os.Exit(exitCode)
	}()

	log.Println("Starting Vault container...")
	var err error
	sharedVaultContainer, err = containers.NewVaultContainer(ctx, nil)
	if err != nil {
		log.Printf("Failed to start Vault container: %v", err)
		exitCode = 1
		return
	}
	log.Printf("Vault container started at %s", sharedVaultContainer.Addr())

	exitCode = m.Run()
}

func requireVault(t *testing.T) *containers.VaultContainer {
	t.Helper()
	if sharedVaultContainer == nil {
		t.Skip("Vault container not available")
	}
	return sharedVaultContainer
}

func TestVaultBackend_Integration_StoreAndRetrieve(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	// Store a PAT secret
	secret := &storage.Secret{
		Type:     "pat",
		PATToken: "ghp_integration_test_token_123",
	}

	ref, err := backend.Store(ctx, secret)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if !strings.HasPrefix(ref, "vault:") {
		t.Errorf("Expected ref to start with 'vault:', got %s", ref)
	}
	t.Logf("Stored secret with ref: %s", ref)

	// Retrieve the secret
	retrieved, err := backend.Retrieve(ctx, ref)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if retrieved.Type != "pat" {
		t.Errorf("Expected type 'pat', got '%s'", retrieved.Type)
	}
	if retrieved.PATToken != "ghp_integration_test_token_123" {
		t.Errorf("Expected PATToken 'ghp_integration_test_token_123', got '%s'", retrieved.PATToken)
	}
}

func TestVaultBackend_Integration_StoreSSHKey(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	// Store an SSH key secret
	secret := &storage.Secret{
		Type:          "ssh",
		SSHPrivateKey: "-----BEGIN RSA PRIVATE KEY-----\ntest-key-content\n-----END RSA PRIVATE KEY-----",
		SSHPassphrase: "test-passphrase",
	}

	ref, err := backend.Store(ctx, secret)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := backend.Retrieve(ctx, ref)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if retrieved.Type != "ssh" {
		t.Errorf("Expected type 'ssh', got '%s'", retrieved.Type)
	}
	if retrieved.SSHPrivateKey != secret.SSHPrivateKey {
		t.Error("SSH private key mismatch")
	}
	if retrieved.SSHPassphrase != secret.SSHPassphrase {
		t.Error("SSH passphrase mismatch")
	}
}

func TestVaultBackend_Integration_Delete(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	// Store a secret
	secret := &storage.Secret{
		Type:     "pat",
		PATToken: "ghp_to_be_deleted",
	}
	ref, err := backend.Store(ctx, secret)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Delete it
	err = backend.Delete(ctx, ref)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = backend.Retrieve(ctx, ref)
	if err == nil {
		t.Error("Expected error when retrieving deleted secret")
	}
}

func TestVaultBackend_Integration_RetrieveNotFound(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	_, err = backend.Retrieve(ctx, "vault:nonexistent-secret-id")
	if err == nil {
		t.Error("Expected error for non-existent secret")
	}
}

func TestVaultBackend_Integration_InvalidReference(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	// Invalid prefix
	_, err = backend.Retrieve(ctx, "secret:some-id")
	if err == nil {
		t.Error("Expected error for invalid prefix")
	}

	// Too short
	_, err = backend.Retrieve(ctx, "vault")
	if err == nil {
		t.Error("Expected error for too-short reference")
	}
}

func TestVaultBackend_Integration_MultipleSecrets(t *testing.T) {
	vault := requireVault(t)

	backend, err := NewVaultBackend(&Config{
		VaultAddress:    vault.Addr(),
		VaultToken:      vault.Token(),
		VaultMountPath:  "secret",
		VaultSecretPath: "github-rag-test",
	})
	if err != nil {
		t.Fatalf("Failed to create Vault backend: %v", err)
	}

	ctx := context.Background()

	// Store multiple secrets
	secrets := []*storage.Secret{
		{Type: "pat", PATToken: "ghp_token_1"},
		{Type: "pat", PATToken: "ghp_token_2"},
		{Type: "ssh", SSHPrivateKey: "key1", SSHPassphrase: "pass1"},
	}

	refs := make([]string, len(secrets))
	for i, s := range secrets {
		ref, err := backend.Store(ctx, s)
		if err != nil {
			t.Fatalf("Store secret %d failed: %v", i, err)
		}
		refs[i] = ref
	}

	// Verify all secrets can be retrieved independently
	for i, ref := range refs {
		retrieved, err := backend.Retrieve(ctx, ref)
		if err != nil {
			t.Fatalf("Retrieve secret %d failed: %v", i, err)
		}
		if retrieved.Type != secrets[i].Type {
			t.Errorf("Secret %d: expected type '%s', got '%s'", i, secrets[i].Type, retrieved.Type)
		}
	}

	// Delete middle secret
	err = backend.Delete(ctx, refs[1])
	if err != nil {
		t.Fatalf("Delete secret 1 failed: %v", err)
	}

	// First and third should still exist
	_, err = backend.Retrieve(ctx, refs[0])
	if err != nil {
		t.Error("Secret 0 should still exist after deleting secret 1")
	}
	_, err = backend.Retrieve(ctx, refs[2])
	if err != nil {
		t.Error("Secret 2 should still exist after deleting secret 1")
	}

	// Second should be gone
	_, err = backend.Retrieve(ctx, refs[1])
	if err == nil {
		t.Error("Secret 1 should be deleted")
	}
}
