package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// generateSSHKeyPEM generates an ed25519 private key in PEM format for tests.
func generateSSHKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	block, err := cryptossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// generateHostKey generates a server host key pair for tests.
func generateHostKey(t *testing.T) cryptossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate host key: %v", err)
	}
	signer, err := cryptossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	return signer
}

func sshSecret(t *testing.T) *storage.Secret {
	t.Helper()
	return &storage.Secret{
		Type:          types.AuthTypeSSH,
		SSHPrivateKey: string(generateSSHKeyPEM(t)),
	}
}

// TestGetAuthMethod_SSHVerifiesHostKeys is the core regression test for the
// InsecureIgnoreHostKey finding: the SSH auth method must verify server host
// keys against a known_hosts file instead of accepting anything.
func TestGetAuthMethod_SSHVerifiesHostKeys(t *testing.T) {
	trustedHost := generateHostKey(t)
	impostorHost := generateHostKey(t)

	// known_hosts trusting only trustedHost for github.com
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	line := "github.com " + string(cryptossh.MarshalAuthorizedKey(trustedHost.PublicKey()))
	if err := os.WriteFile(knownHostsPath, []byte(line), 0o600); err != nil {
		t.Fatalf("failed to write known_hosts: %v", err)
	}
	t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

	auth, err := GetAuthMethod(sshSecret(t))
	if err != nil {
		t.Fatalf("GetAuthMethod failed: %v", err)
	}

	publicKeys, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatalf("expected *ssh.PublicKeys, got %T", auth)
	}
	if publicKeys.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback must be set to a verifying callback")
	}

	addr := &net.TCPAddr{IP: net.IPv4(140, 82, 121, 4), Port: 22}

	// The trusted host key must be accepted.
	if err := publicKeys.HostKeyCallback("github.com:22", addr, trustedHost.PublicKey()); err != nil {
		t.Errorf("expected trusted host key to be accepted, got: %v", err)
	}

	// An impostor's host key must be rejected (this passed silently with
	// InsecureIgnoreHostKey).
	if err := publicKeys.HostKeyCallback("github.com:22", addr, impostorHost.PublicKey()); err == nil {
		t.Error("expected unknown host key to be rejected, but it was accepted")
	}

	// A host that is not in known_hosts at all must be rejected.
	if err := publicKeys.HostKeyCallback("evil.example.com:22", addr, impostorHost.PublicKey()); err == nil {
		t.Error("expected unknown host to be rejected, but it was accepted")
	}
}

// TestGetAuthMethod_SSHFailsClosedWithoutKnownHosts verifies that when no
// known_hosts file is available at all, SSH auth setup fails instead of
// silently skipping verification.
func TestGetAuthMethod_SSHFailsClosedWithoutKnownHosts(t *testing.T) {
	// Point every known_hosts source at nonexistent locations.
	emptyHome := t.TempDir()
	t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(emptyHome, "does-not-exist"))

	_, err := GetAuthMethod(sshSecret(t))
	if err == nil {
		t.Fatal("expected an error when no known_hosts file is available (fail closed)")
	}
}

// TestGetAuthMethod_PATUnaffected ensures PAT auth still works.
func TestGetAuthMethod_PATUnaffected(t *testing.T) {
	auth, err := GetAuthMethod(&storage.Secret{Type: types.AuthTypePAT, PATToken: "token"})
	if err != nil {
		t.Fatalf("PAT auth failed: %v", err)
	}
	if auth == nil {
		t.Fatal("expected an auth method for PAT")
	}
}

// TestGetAuthMethod_PublicRepoUnaffected ensures public repos need no auth.
func TestGetAuthMethod_PublicRepoUnaffected(t *testing.T) {
	auth, err := GetAuthMethod(nil)
	if err != nil {
		t.Fatalf("public repo auth failed: %v", err)
	}
	if auth != nil {
		t.Fatal("expected nil auth method for public repos")
	}
}
