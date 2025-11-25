package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/google/uuid"
)

const (
	repoKeyPrefix = "github-rag:repo:"
	repoIndexKey  = "github-rag:repos:index"
)

// RepositoryStore manages repository persistence
type RepositoryStore struct {
	kv *KVStore
}

// NewRepositoryStore creates a new repository store
func NewRepositoryStore(kv *KVStore) *RepositoryStore {
	return &RepositoryStore{kv: kv}
}

// Create creates a new repository
func (s *RepositoryStore) Create(ctx context.Context, repo *types.Repository) error {
	// Generate ID if not set
	if repo.ID == "" {
		repo.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	repo.CreatedAt = now
	repo.UpdatedAt = now

	// Validate
	if err := repo.Validate(); err != nil {
		return err
	}

	// Store repository
	key := repoKeyPrefix + repo.ID
	if err := s.kv.Write(ctx, key, repo, nil); err != nil {
		return fmt.Errorf("failed to write repository: %w", err)
	}

	// Add to index
	if err := s.addToIndex(ctx, repo.ID); err != nil {
		// Rollback
		s.kv.Delete(ctx, key)
		return fmt.Errorf("failed to update index: %w", err)
	}

	return nil
}

// Get retrieves a repository by ID
func (s *RepositoryStore) Get(ctx context.Context, id string) (*types.Repository, error) {
	var repo types.Repository
	key := repoKeyPrefix + id

	if err := s.kv.Read(ctx, key, &repo); err != nil {
		return nil, types.ErrRepositoryNotFound
	}

	return &repo, nil
}

// Update updates an existing repository
func (s *RepositoryStore) Update(ctx context.Context, repo *types.Repository) error {
	// Check if exists
	existing, err := s.Get(ctx, repo.ID)
	if err != nil {
		return err
	}

	// Preserve created timestamp
	repo.CreatedAt = existing.CreatedAt
	repo.UpdatedAt = time.Now()

	// Validate
	if err := repo.Validate(); err != nil {
		return err
	}

	// Store
	key := repoKeyPrefix + repo.ID
	return s.kv.Write(ctx, key, repo, nil)
}

// Delete deletes a repository
func (s *RepositoryStore) Delete(ctx context.Context, id string) error {
	// Check if exists
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	// Delete repository
	key := repoKeyPrefix + id
	if err := s.kv.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to delete repository: %w", err)
	}

	// Remove from index
	if err := s.removeFromIndex(ctx, id); err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}

	return nil
}

// List lists all repositories
func (s *RepositoryStore) List(ctx context.Context) ([]*types.Repository, error) {
	// Read index
	ids, err := s.getIndex(ctx)
	if err != nil {
		return []*types.Repository{}, nil // Empty list if index doesn't exist
	}

	// Load each repository
	repos := make([]*types.Repository, 0, len(ids))
	for _, id := range ids {
		repo, err := s.Get(ctx, id)
		if err != nil {
			// Skip missing repos (cleanup race condition)
			continue
		}
		repos = append(repos, repo)
	}

	return repos, nil
}

// getIndex retrieves the repository ID index
func (s *RepositoryStore) getIndex(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.kv.Read(ctx, repoIndexKey, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// addToIndex adds a repository ID to the index
func (s *RepositoryStore) addToIndex(ctx context.Context, id string) error {
	ids, err := s.getIndex(ctx)
	if err != nil {
		// Index doesn't exist, create it
		ids = []string{}
	}

	// Check if already exists
	for _, existingID := range ids {
		if existingID == id {
			return nil // Already in index
		}
	}

	// Add and save
	ids = append(ids, id)
	return s.kv.Write(ctx, repoIndexKey, ids, nil)
}

// removeFromIndex removes a repository ID from the index
func (s *RepositoryStore) removeFromIndex(ctx context.Context, id string) error {
	ids, err := s.getIndex(ctx)
	if err != nil {
		return nil // Index doesn't exist, nothing to remove
	}

	// Filter out the ID
	newIDs := make([]string, 0, len(ids))
	for _, existingID := range ids {
		if existingID != id {
			newIDs = append(newIDs, existingID)
		}
	}

	return s.kv.Write(ctx, repoIndexKey, newIDs, nil)
}
