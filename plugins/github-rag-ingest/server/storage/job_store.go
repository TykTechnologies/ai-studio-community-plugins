package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/google/uuid"
)

const (
	jobKeyPrefix     = "github-rag:job:"
	jobLogsKeyPrefix = "github-rag:job:"
	jobLogsKeySuffix = ":logs"
	jobsByRepoPrefix = "github-rag:jobs:repo:"
	jobsIndexKey     = "github-rag:jobs:index"
)

// JobStore manages job persistence
type JobStore struct {
	kv *KVStore
}

// NewJobStore creates a new job store
func NewJobStore(kv *KVStore) *JobStore {
	return &JobStore{kv: kv}
}

// Create creates a new job
func (s *JobStore) Create(ctx context.Context, job *types.Job) error {
	// Generate ID if not set
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	// Store job
	key := jobKeyPrefix + job.ID
	if err := s.kv.Write(ctx, key, job, nil); err != nil {
		return fmt.Errorf("failed to write job: %w", err)
	}

	// Add to the repo index (for filtering) and the global index. If either
	// fails the job is removed again, so no job exists that the lists miss.
	for _, indexKey := range []string{jobsByRepoPrefix + job.RepoID, jobsIndexKey} {
		if err := s.appendToIndex(ctx, indexKey, job.ID); err != nil {
			if delErr := s.kv.Delete(ctx, key); delErr != nil {
				log.Printf("github-rag-ingest: job %s is stored but not indexed; removing it failed: %v", job.ID, delErr)
			}
			return fmt.Errorf("failed to index job %s: %w", job.ID, err)
		}
	}

	return nil
}

// appendToIndex adds id to the ID list stored at indexKey. A missing list
// starts empty; any other read error aborts, because writing back a list
// that failed to load would drop every ID already in it.
func (s *JobStore) appendToIndex(ctx context.Context, indexKey, id string) error {
	var ids []string
	if err := s.kv.ReadIfExists(ctx, indexKey, &ids); err != nil {
		return fmt.Errorf("read index %s: %w", indexKey, err)
	}
	if err := s.kv.Write(ctx, indexKey, append(ids, id), nil); err != nil {
		return fmt.Errorf("write index %s: %w", indexKey, err)
	}
	return nil
}

// Get retrieves a job by ID
func (s *JobStore) Get(ctx context.Context, id string) (*types.Job, error) {
	var job types.Job
	key := jobKeyPrefix + id

	if err := s.kv.Read(ctx, key, &job); err != nil {
		return nil, types.ErrJobNotFound
	}

	return &job, nil
}

// Update updates an existing job
func (s *JobStore) Update(ctx context.Context, job *types.Job) error {
	// Check if exists
	if _, err := s.Get(ctx, job.ID); err != nil {
		return err
	}

	// Store
	key := jobKeyPrefix + job.ID
	return s.kv.Write(ctx, key, job, nil)
}

// ListByRepo lists all jobs for a repository
func (s *JobStore) ListByRepo(ctx context.Context, repoID string, limit, offset int) ([]*types.Job, int, error) {
	// Get job IDs for repo
	repoJobsKey := jobsByRepoPrefix + repoID
	var jobIDs []string
	if err := s.kv.Read(ctx, repoJobsKey, &jobIDs); err != nil {
		return []*types.Job{}, 0, nil // No jobs for this repo
	}

	totalCount := len(jobIDs)

	// Apply pagination
	start := offset
	end := offset + limit
	if start >= len(jobIDs) {
		return []*types.Job{}, totalCount, nil
	}
	if end > len(jobIDs) {
		end = len(jobIDs)
	}

	// Load jobs
	jobs := make([]*types.Job, 0, end-start)
	for i := start; i < end; i++ {
		job, err := s.Get(ctx, jobIDs[i])
		if err != nil {
			continue // Skip missing jobs
		}
		jobs = append(jobs, job)
	}

	return jobs, totalCount, nil
}

// ListAll lists all jobs across all repositories
func (s *JobStore) ListAll(ctx context.Context, limit, offset int) ([]*types.Job, int, error) {
	// Get all job IDs from global index
	var jobIDs []string
	if err := s.kv.Read(ctx, jobsIndexKey, &jobIDs); err != nil {
		return []*types.Job{}, 0, nil // No jobs yet
	}

	totalCount := len(jobIDs)

	// Apply pagination
	start := offset
	end := offset + limit
	if start >= len(jobIDs) {
		return []*types.Job{}, totalCount, nil
	}
	if end > len(jobIDs) {
		end = len(jobIDs)
	}

	// Reverse order to show newest first
	reversedIDs := make([]string, len(jobIDs))
	for i, id := range jobIDs {
		reversedIDs[len(jobIDs)-1-i] = id
	}

	// Load jobs
	jobs := make([]*types.Job, 0, end-start)
	for i := start; i < end; i++ {
		job, err := s.Get(ctx, reversedIDs[i])
		if err != nil {
			continue // Skip missing jobs
		}
		jobs = append(jobs, job)
	}

	return jobs, totalCount, nil
}

// AddLog adds a log entry to a job
func (s *JobStore) AddLog(ctx context.Context, log *types.JobLog) error {
	// Get existing logs
	logsKey := jobLogsKeyPrefix + log.JobID + jobLogsKeySuffix
	var logs []types.JobLog
	// Writing back after a failed read would drop the existing logs
	if err := s.kv.ReadIfExists(ctx, logsKey, &logs); err != nil {
		return fmt.Errorf("read logs for job %s: %w", log.JobID, err)
	}

	// Append new log
	logs = append(logs, *log)

	// Store with TTL (30 days)
	ttl := 30 * 24 * time.Hour
	return s.kv.Write(ctx, logsKey, logs, &ttl)
}

// GetLogs retrieves logs for a job
func (s *JobStore) GetLogs(ctx context.Context, jobID string, level string, limit, offset int) ([]types.JobLog, int, error) {
	logsKey := jobLogsKeyPrefix + jobID + jobLogsKeySuffix
	var logs []types.JobLog

	if err := s.kv.Read(ctx, logsKey, &logs); err != nil {
		return []types.JobLog{}, 0, nil // No logs yet
	}

	// Filter by level if specified
	var filtered []types.JobLog
	if level != "" {
		for _, log := range logs {
			if log.Level == level {
				filtered = append(filtered, log)
			}
		}
	} else {
		filtered = logs
	}

	totalCount := len(filtered)

	// Apply pagination
	start := offset
	end := offset + limit
	if start >= len(filtered) {
		return []types.JobLog{}, totalCount, nil
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], totalCount, nil
}

// DeleteJobLogs deletes logs for a job
func (s *JobStore) DeleteJobLogs(ctx context.Context, jobID string) error {
	logsKey := jobLogsKeyPrefix + jobID + jobLogsKeySuffix
	return s.kv.Delete(ctx, logsKey)
}
