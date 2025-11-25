package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// TriggerIngestion starts an ingestion job
func (h *Handler) TriggerIngestion(payload []byte) ([]byte, error) {
	var req TriggerIngestionRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()

	// Validate repository exists
	repo, err := h.repoStore.Get(ctx, req.RepoID)
	if err != nil {
		return errorResponse("repository not found")
	}

	// Default to incremental if not specified
	if req.Type == "" {
		req.Type = types.JobTypeIncremental
	}

	// Create job
	job := types.NewJob(repo.ID, req.Type, types.TriggerManual, req.DryRun)
	if err := h.jobStore.Create(ctx, job); err != nil {
		return errorResponse(fmt.Sprintf("failed to create job: %v", err))
	}

	// Run ingestion asynchronously
	go func() {
		bgCtx := context.Background()
		if err := h.engine.Run(bgCtx, job); err != nil {
			fmt.Printf("Ingestion job %s failed: %v\n", job.ID, err)
		}
	}()

	return successResponse(map[string]interface{}{
		"job_id":  job.ID,
		"message": "ingestion job started",
	})
}

// ListJobs lists jobs with optional filtering
func (h *Handler) ListJobs(payload []byte) ([]byte, error) {
	var req struct {
		RepoID string `json:"repo_id,omitempty"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	if req.Limit == 0 {
		req.Limit = 20
	}

	ctx := context.Background()

	var jobs []*types.Job
	var totalCount int
	var err error

	if req.RepoID != "" {
		// List jobs for specific repository
		jobs, totalCount, err = h.jobStore.ListByRepo(ctx, req.RepoID, req.Limit, req.Offset)
	} else {
		// List all jobs across all repositories
		jobs, totalCount, err = h.jobStore.ListAll(ctx, req.Limit, req.Offset)
	}

	if err != nil {
		return errorResponse(fmt.Sprintf("failed to list jobs: %v", err))
	}

	return successResponse(map[string]interface{}{
		"jobs":        jobs,
		"total_count": totalCount,
	})
}

// GetJob returns job details
func (h *Handler) GetJob(payload []byte) ([]byte, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()
	job, err := h.jobStore.Get(ctx, req.ID)
	if err != nil {
		return errorResponse("job not found")
	}

	return successResponse(job)
}

// GetJobLogs returns job logs
func (h *Handler) GetJobLogs(payload []byte) ([]byte, error) {
	var req GetJobLogsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	if req.Limit == 0 {
		req.Limit = 100
	}

	ctx := context.Background()
	logs, totalCount, err := h.jobStore.GetLogs(ctx, req.JobID, req.Level, req.Limit, req.Offset)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to get logs: %v", err))
	}

	return successResponse(map[string]interface{}{
		"logs":        logs,
		"total_count": totalCount,
	})
}

// CancelJob cancels a running job
func (h *Handler) CancelJob(payload []byte) ([]byte, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request")
	}

	ctx := context.Background()

	// Get job
	job, err := h.jobStore.Get(ctx, req.ID)
	if err != nil {
		return errorResponse("job not found")
	}

	// Check if job is running
	if job.Status != types.JobStatusRunning && job.Status != types.JobStatusQueued {
		return errorResponse("job is not running")
	}

	// Update status to cancelled
	job.Complete(types.JobStatusCancelled, "cancelled by user")
	if err := h.jobStore.Update(ctx, job); err != nil {
		return errorResponse(fmt.Sprintf("failed to cancel job: %v", err))
	}

	return successResponse(map[string]interface{}{
		"message": "job cancelled",
	})
}
