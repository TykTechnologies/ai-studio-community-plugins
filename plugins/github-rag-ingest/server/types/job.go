package types

import "time"

// Job represents an ingestion job
type Job struct {
	ID          string       `json:"id"`
	RepoID      string       `json:"repo_id"`
	Type        string       `json:"type"` // full, incremental
	Status      string       `json:"status"`
	DryRun      bool         `json:"dry_run"`
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	FromCommit  string       `json:"from_commit"`
	ToCommit    string       `json:"to_commit"`
	Stats       JobStats     `json:"stats"`
	ErrorMessage string      `json:"error_message,omitempty"`
	TriggeredBy string       `json:"triggered_by"` // manual, schedule
}

// JobStats tracks statistics for a job
type JobStats struct {
	FilesScanned  int `json:"files_scanned"`
	FilesAdded    int `json:"files_added"`
	FilesChanged  int `json:"files_changed"`
	FilesDeleted  int `json:"files_deleted"`
	FilesSkipped  int `json:"files_skipped"`
	ChunksWritten int `json:"chunks_written"`
	ChunksDeleted int `json:"chunks_deleted"`
	Errors        int `json:"errors"`
}

// JobLog represents a log entry for a job
type JobLog struct {
	JobID     string    `json:"job_id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // info, warn, error, debug
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"`
}

// Job type constants
const (
	JobTypeFull        = "full"
	JobTypeIncremental = "incremental"
)

// Job status constants
const (
	JobStatusQueued  = "queued"
	JobStatusRunning = "running"
	JobStatusSuccess = "success"
	JobStatusFailed  = "failed"
	JobStatusPartial = "partial"
	JobStatusCancelled = "cancelled"
)

// Job trigger constants
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
)

// Log level constants
const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
	LogLevelDebug = "debug"
)

// NewJob creates a new job
func NewJob(repoID, jobType, triggeredBy string, dryRun bool) *Job {
	return &Job{
		RepoID:      repoID,
		Type:        jobType,
		Status:      JobStatusQueued,
		DryRun:      dryRun,
		StartedAt:   time.Now(),
		TriggeredBy: triggeredBy,
		Stats:       JobStats{},
	}
}

// Complete marks the job as completed
func (j *Job) Complete(status string, errorMessage string) {
	j.Status = status
	j.ErrorMessage = errorMessage
	now := time.Now()
	j.CompletedAt = &now
}

// IsComplete returns true if the job has finished
func (j *Job) IsComplete() bool {
	return j.CompletedAt != nil
}

// Duration returns the job duration
func (j *Job) Duration() time.Duration {
	if j.CompletedAt != nil {
		return j.CompletedAt.Sub(j.StartedAt)
	}
	return time.Since(j.StartedAt)
}
