package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Client wraps git operations for repository management
type Client struct {
	cacheDir string
}

// NewClient creates a new Git client
func NewClient(cacheDir string) (*Client, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Client{
		cacheDir: cacheDir,
	}, nil
}

// CloneOrFetch clones a repository or fetches updates if already cloned
func (c *Client) CloneOrFetch(ctx context.Context, repo *types.Repository, secret *storage.Secret) (*git.Repository, error) {
	repoPath := c.getRepoPath(repo.ID)

	// Check if repository already exists
	gitRepo, err := git.PlainOpen(repoPath)
	if err == nil {
		// Repository exists, fetch updates
		return c.fetch(gitRepo, secret)
	}

	// Repository doesn't exist, clone it
	return c.clone(ctx, repo, secret, repoPath)
}

// clone clones a new repository
func (c *Client) clone(ctx context.Context, repo *types.Repository, secret *storage.Secret, repoPath string) (*git.Repository, error) {
	auth, err := GetAuthMethod(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth method: %w", err)
	}

	opts := CloneOptions(repo.URL, auth, repo.Branch)

	gitRepo, err := git.PlainCloneContext(ctx, repoPath, false, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", types.ErrGitCloneFailed, err)
	}

	return gitRepo, nil
}

// fetch fetches updates for an existing repository
func (c *Client) fetch(gitRepo *git.Repository, secret *storage.Secret) (*git.Repository, error) {
	auth, err := GetAuthMethod(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth method: %w", err)
	}

	opts := FetchOptions(auth)

	if err := gitRepo.Fetch(opts); err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return gitRepo, nil // Already up to date
		}
		return nil, fmt.Errorf("%w: %v", types.ErrGitFetchFailed, err)
	}

	return gitRepo, nil
}

// GetHead returns the current HEAD commit SHA
func (c *Client) GetHead(gitRepo *git.Repository, branch string) (string, error) {
	ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return "", fmt.Errorf("failed to get branch reference: %w", err)
	}

	return ref.Hash().String(), nil
}

// getRepoPath returns the local cache path for a repository
func (c *Client) getRepoPath(repoID string) string {
	return filepath.Join(c.cacheDir, repoID)
}

// CleanCache removes cached repository
func (c *Client) CleanCache(repoID string) error {
	repoPath := c.getRepoPath(repoID)
	return os.RemoveAll(repoPath)
}
