package git

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// knownHostsCallback builds a host-key verification callback from known_hosts
// files. GITHUB_RAG_KNOWN_HOSTS takes precedence (a list of paths in the
// platform's path-list format); otherwise go-git's defaults apply
// (SSH_KNOWN_HOSTS, then ~/.ssh/known_hosts and /etc/ssh/ssh_known_hosts).
// It fails closed: if no known_hosts file is available, an error is returned
// rather than skipping verification.
func knownHostsCallback() (cryptossh.HostKeyCallback, error) {
	if paths := os.Getenv("GITHUB_RAG_KNOWN_HOSTS"); paths != "" {
		return ssh.NewKnownHostsCallback(filepath.SplitList(paths)...)
	}
	return ssh.NewKnownHostsCallback()
}

// GetAuthMethod returns the appropriate git authentication method
func GetAuthMethod(secret *storage.Secret) (transport.AuthMethod, error) {
	if secret == nil {
		return nil, nil // Public repository
	}

	switch secret.Type {
	case types.AuthTypePAT:
		return &http.BasicAuth{
			Username: "x-access-token", // GitHub convention for PAT
			Password: secret.PATToken,
		}, nil

	case types.AuthTypeSSH:
		publicKeys, err := ssh.NewPublicKeys("git", []byte(secret.SSHPrivateKey), secret.SSHPassphrase)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
		}

		// Verify server host keys against known_hosts. Without this an
		// attacker who can redirect or MITM the SSH connection could
		// impersonate the Git server, capture credentials, or serve
		// malicious repository content.
		callback, err := knownHostsCallback()
		if err != nil {
			return nil, fmt.Errorf("SSH host-key verification requires a known_hosts file (set GITHUB_RAG_KNOWN_HOSTS or SSH_KNOWN_HOSTS, or provide ~/.ssh/known_hosts): %w", err)
		}
		publicKeys.HostKeyCallback = callback

		return publicKeys, nil

	case types.AuthTypePublic:
		return nil, nil

	default:
		return nil, types.ErrInvalidAuthType
	}
}

// CloneOptions returns clone options with authentication
func CloneOptions(url string, auth transport.AuthMethod, branch string) *git.CloneOptions {
	opts := &git.CloneOptions{
		URL:      url,
		Auth:     auth,
		Progress: nil, // TODO: Add progress tracking
	}

	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}

	return opts
}

// FetchOptions returns fetch options with authentication
func FetchOptions(auth transport.AuthMethod) *git.FetchOptions {
	return &git.FetchOptions{
		Auth:     auth,
		Progress: nil, // TODO: Add progress tracking
		Force:    true,
	}
}
