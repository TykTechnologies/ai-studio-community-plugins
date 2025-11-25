package rpc

import (
	"context"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// GetStatistics returns overall plugin statistics
func (h *Handler) GetStatistics(payload []byte) ([]byte, error) {
	ctx := context.Background()

	// Get all repositories
	repos, err := h.repoStore.List(ctx)
	if err != nil {
		return errorResponse("failed to load repositories")
	}

	// Count active repos
	activeRepos := 0
	for _, repo := range repos {
		if repo.SyncEnabled {
			activeRepos++
		}
	}

	// Get all jobs
	allJobs, totalJobs, err := h.jobStore.ListAll(ctx, 1000, 0) // Get up to 1000 jobs for stats
	if err != nil {
		totalJobs = 0
		allJobs = nil
	}

	// Calculate total chunks ingested
	totalChunks := 0
	recentJobs := 0
	if allJobs != nil {
		// Last 10 jobs
		if len(allJobs) > 10 {
			recentJobs = 10
		} else {
			recentJobs = len(allJobs)
		}

		// Sum chunks from all successful jobs
		for _, job := range allJobs {
			if job.Status == types.JobStatusSuccess {
				totalChunks += job.Stats.ChunksWritten
			}
		}
	}

	stats := map[string]interface{}{
		"total_repos":     len(repos),
		"active_repos":    activeRepos,
		"total_jobs":      totalJobs,
		"chunks_ingested": totalChunks,
		"recent_jobs":     recentJobs,
	}

	return successResponse(stats)
}
