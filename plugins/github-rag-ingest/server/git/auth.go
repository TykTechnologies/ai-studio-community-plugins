package git

import (
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

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

		// Configure host key callback to accept any host key
		// In production, you might want to verify against known hosts
		publicKeys.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()

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
