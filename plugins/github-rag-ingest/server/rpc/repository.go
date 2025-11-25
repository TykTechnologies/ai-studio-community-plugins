package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
)

// ListRepositories returns all repositories
func (h *Handler) ListRepositories(payload []byte) ([]byte, error) {
	ctx := context.Background()

	repos, err := h.repoStore.List(ctx)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to list repositories: %v", err))
	}

	return successResponse(map[string]interface{}{
		"repositories": repos,
		"count":        len(repos),
	})
}

// GetRepository returns a single repository
func (h *Handler) GetRepository(payload []byte) ([]byte, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()
	repo, err := h.repoStore.Get(ctx, req.ID)
	if err != nil {
		return errorResponse(fmt.Sprintf("repository not found: %v", err))
	}

	return successResponse(repo)
}

// CreateRepository creates a new repository
func (h *Handler) CreateRepository(payload []byte) ([]byte, error) {
	var req CreateRepositoryRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()

	// Create repository
	repo := types.NewRepository(req.Name, req.URL, req.Branch)
	repo.Owner = req.Owner
	repo.AuthType = req.AuthType
	repo.DatasourceID = req.DatasourceID
	repo.TargetPaths = req.TargetPaths
	repo.FileMasks = req.FileMasks
	repo.IgnorePatterns = req.IgnorePatterns
	repo.ChunkingStrategy = req.ChunkingStrategy
	repo.ChunkSize = req.ChunkSize
	repo.ChunkOverlap = req.ChunkOverlap
	repo.SyncSchedule = req.SyncSchedule
	repo.SyncEnabled = req.SyncEnabled

	// Generate namespace
	repo.Namespace = fmt.Sprintf("github-%s-%s", sanitizeName(req.Name), req.Branch)

	// Store authentication secret if provided
	if req.AuthType != types.AuthTypePublic {
		secret := &storage.Secret{
			Type: req.AuthType,
		}

		switch req.AuthType {
		case types.AuthTypePAT:
			secret.PATToken = req.PATToken
		case types.AuthTypeSSH:
			secret.SSHPrivateKey = req.SSHPrivateKey
			secret.SSHPassphrase = req.SSHPassphrase
		}

		secretRef, err := h.secretBackend.Store(ctx, secret)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to store secret: %v", err))
		}

		repo.AuthSecretRef = secretRef
	}

	// Save repository
	if err := h.repoStore.Create(ctx, repo); err != nil {
		return errorResponse(fmt.Sprintf("failed to create repository: %v", err))
	}

	// Create schedule if sync enabled
	if repo.SyncEnabled && repo.SyncSchedule != "" {
		scheduleID := fmt.Sprintf("github-rag:sync:%s", repo.ID)
		_, err := ai_studio_sdk.CreateSchedule(
			ctx,
			scheduleID,
			fmt.Sprintf("Sync %s/%s", repo.Owner, repo.Name),
			repo.SyncSchedule,
			"UTC",
			300, // 5 minute timeout
			map[string]interface{}{"repo_id": repo.ID},
			true,
		)
		if err != nil {
			// Log error but don't fail the repository creation
			// Schedule can be created later
		}
	}

	return successResponse(repo)
}

// UpdateRepository updates an existing repository
func (h *Handler) UpdateRepository(payload []byte) ([]byte, error) {
	var req CreateRepositoryRequest
	var reqMap map[string]interface{}

	if err := json.Unmarshal(payload, &reqMap); err != nil {
		return errorResponse("invalid request")
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	repoID, ok := reqMap["id"].(string)
	if !ok {
		return errorResponse("repository id is required")
	}

	ctx := context.Background()

	// Get existing repository
	repo, err := h.repoStore.Get(ctx, repoID)
	if err != nil {
		return errorResponse("repository not found")
	}

	// Update fields
	repo.Name = req.Name
	repo.Owner = req.Owner
	repo.URL = req.URL
	repo.Branch = req.Branch
	repo.DatasourceID = req.DatasourceID
	repo.TargetPaths = req.TargetPaths
	repo.FileMasks = req.FileMasks
	repo.IgnorePatterns = req.IgnorePatterns
	repo.ChunkingStrategy = req.ChunkingStrategy
	repo.ChunkSize = req.ChunkSize
	repo.ChunkOverlap = req.ChunkOverlap

	// Update sync schedule if changed
	scheduleChanged := repo.SyncSchedule != req.SyncSchedule || repo.SyncEnabled != req.SyncEnabled
	repo.SyncSchedule = req.SyncSchedule
	repo.SyncEnabled = req.SyncEnabled

	// Update auth if provided
	if req.AuthType != types.AuthTypePublic && req.AuthType != "" {
		// Delete old secret if it exists
		if repo.AuthSecretRef != "" {
			h.secretBackend.Delete(ctx, repo.AuthSecretRef)
		}

		// Create new secret
		secret := &storage.Secret{Type: req.AuthType}
		switch req.AuthType {
		case types.AuthTypePAT:
			secret.PATToken = req.PATToken
		case types.AuthTypeSSH:
			secret.SSHPrivateKey = req.SSHPrivateKey
			secret.SSHPassphrase = req.SSHPassphrase
		}

		secretRef, err := h.secretBackend.Store(ctx, secret)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to store secret: %v", err))
		}

		repo.AuthSecretRef = secretRef
		repo.AuthType = req.AuthType
	}

	// Save repository
	if err := h.repoStore.Update(ctx, repo); err != nil {
		return errorResponse(fmt.Sprintf("failed to update repository: %v", err))
	}

	// Update schedule if needed
	if scheduleChanged {
		scheduleID := fmt.Sprintf("github-rag:sync:%s", repo.ID)

		if repo.SyncEnabled && repo.SyncSchedule != "" {
			// Try to update existing schedule, or create new one
			enabled := true
			_, err := ai_studio_sdk.UpdateSchedule(ctx, scheduleID, ai_studio_sdk.UpdateScheduleOptions{
				CronExpr: &repo.SyncSchedule,
				Enabled:  &enabled,
			})

			if err != nil {
				// Schedule doesn't exist, create it
				ai_studio_sdk.CreateSchedule(
					ctx, scheduleID,
					fmt.Sprintf("Sync %s/%s", repo.Owner, repo.Name),
					repo.SyncSchedule, "UTC", 300,
					map[string]interface{}{"repo_id": repo.ID},
					true,
				)
			}
		} else {
			// Disable or delete schedule
			ai_studio_sdk.DeleteSchedule(ctx, scheduleID)
		}
	}

	return successResponse(repo)
}

// DeleteRepository deletes a repository
func (h *Handler) DeleteRepository(payload []byte) ([]byte, error) {
	var req struct {
		ID           string `json:"id"`
		DeleteChunks bool   `json:"delete_chunks"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()

	// Get repository
	repo, err := h.repoStore.Get(ctx, req.ID)
	if err != nil {
		return errorResponse("repository not found")
	}

	// Delete chunks if requested
	if req.DeleteChunks {
		metadata := map[string]string{"repo_id": repo.ID}
		count, err := ai_studio_sdk.DeleteDocumentsByMetadata(ctx, repo.DatasourceID, metadata, "AND", false)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to delete chunks: %v", err))
		}
		fmt.Printf("Deleted %d chunks for repository %s\n", count, repo.Name)
	}

	// Delete schedule if exists
	scheduleID := fmt.Sprintf("github-rag:sync:%s", repo.ID)
	ai_studio_sdk.DeleteSchedule(ctx, scheduleID) // Ignore error if doesn't exist

	// Delete secret if exists
	if repo.AuthSecretRef != "" {
		h.secretBackend.Delete(ctx, repo.AuthSecretRef) // Ignore error
	}

	// Delete repository
	if err := h.repoStore.Delete(ctx, repo.ID); err != nil {
		return errorResponse(fmt.Sprintf("failed to delete repository: %v", err))
	}

	// Clean cache
	h.gitClient.CleanCache(repo.ID)

	return successResponse(map[string]interface{}{
		"message": "repository deleted successfully",
	})
}

// Helper to sanitize names for namespaces
func sanitizeName(name string) string {
	// Remove special characters and replace spaces with dashes
	result := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			result += string(ch)
		} else if ch == ' ' || ch == '-' || ch == '_' {
			result += "-"
		}
	}
	return result
}
