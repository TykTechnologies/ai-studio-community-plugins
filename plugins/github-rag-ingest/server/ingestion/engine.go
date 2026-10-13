package ingestion

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/git"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	gogit "github.com/go-git/go-git/v5"
)

// ErrJobCancelled is returned by Run for a job cancelled with Cancel.
var ErrJobCancelled = errors.New("job cancelled")

// repoSource clones or fetches repositories (*git.Client).
type repoSource interface {
	CloneOrFetch(ctx context.Context, repo *types.Repository, secret *storage.Secret) (*gogit.Repository, error)
	GetHead(gitRepo *gogit.Repository, branch string) (string, error)
}

// chunkStore writes and deletes chunks in a datasource (the AI Studio SDK).
type chunkStore interface {
	ProcessAndStoreDocuments(ctx context.Context, datasourceID uint32, chunks []*mgmtpb.DocumentChunk) (*mgmtpb.ProcessAndStoreResponse, error)
	DeleteDocumentsByMetadata(ctx context.Context, datasourceID uint32, metadataFilter map[string]string, filterMode string, dryRun bool) (int32, error)
}

// sdkChunkStore is the chunkStore backed by the AI Studio service API.
type sdkChunkStore struct{}

func (sdkChunkStore) ProcessAndStoreDocuments(ctx context.Context, datasourceID uint32, chunks []*mgmtpb.DocumentChunk) (*mgmtpb.ProcessAndStoreResponse, error) {
	return ai_studio_sdk.ProcessAndStoreDocuments(ctx, datasourceID, chunks)
}

func (sdkChunkStore) DeleteDocumentsByMetadata(ctx context.Context, datasourceID uint32, metadataFilter map[string]string, filterMode string, dryRun bool) (int32, error) {
	return ai_studio_sdk.DeleteDocumentsByMetadata(ctx, datasourceID, metadataFilter, filterMode, dryRun)
}

// Engine orchestrates the ingestion process
type Engine struct {
	gitClient   repoSource
	chunks      chunkStore
	secretStore *storage.SecretStore
	jobStore    *storage.JobStore
	repoStore   *storage.RepositoryStore

	// Running jobs by ID, so Cancel can stop them, and jobs cancelled
	// before Run registered them, so Run does not start them.
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	cancelled map[string]struct{}
}

// NewEngine creates a new ingestion engine
func NewEngine(gitClient *git.Client, secretStore *storage.SecretStore, jobStore *storage.JobStore, repoStore *storage.RepositoryStore) *Engine {
	return newEngine(gitClient, sdkChunkStore{}, secretStore, jobStore, repoStore)
}

func newEngine(source repoSource, chunks chunkStore, secretStore *storage.SecretStore, jobStore *storage.JobStore, repoStore *storage.RepositoryStore) *Engine {
	return &Engine{
		gitClient:   source,
		chunks:      chunks,
		secretStore: secretStore,
		jobStore:    jobStore,
		repoStore:   repoStore,
		running:     map[string]context.CancelFunc{},
		cancelled:   map[string]struct{}{},
	}
}

// Cancel stops job id if it is running in this engine and reports whether
// it was. A job not running yet is marked, so Run ends it as cancelled
// instead of starting it.
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cancel, ok := e.running[id]; ok {
		cancel()
		return true
	}
	e.cancelled[id] = struct{}{}
	return false
}

// register makes job cancellable. It returns false if the job was
// cancelled before it started.
func (e *Engine) register(id string, cancel context.CancelFunc) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.cancelled[id]; ok {
		delete(e.cancelled, id)
		return false
	}
	e.running[id] = cancel
	return true
}

func (e *Engine) unregister(id string) {
	e.mu.Lock()
	delete(e.running, id)
	e.mu.Unlock()
}

// Run executes an ingestion job. Cancel stops it between files and batches
// (and interrupts clones and datasource calls); the job then ends as
// cancelled, and the repository's last-synced commit is not advanced, so the
// next incremental sync covers the files the cancelled run did not finish.
func (e *Engine) Run(parent context.Context, job *types.Job) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Job records and logs are written even after cancellation
	storeCtx := context.WithoutCancel(parent)

	// reason says who stopped a cancelled job: a user (Cancel) or the host
	// (the caller's context, e.g. a scheduler timeout)
	reason := func() string {
		if err := parent.Err(); err != nil {
			return "stopped by the host: " + err.Error()
		}
		return "cancelled by user"
	}

	if !e.register(job.ID, cancel) {
		return e.cancelJob(storeCtx, job, reason())
	}
	defer e.unregister(job.ID)

	// stop ends the job as failed, or as cancelled if that is why it stopped
	stop := func(msg string) error {
		if ctx.Err() != nil {
			return e.cancelJob(storeCtx, job, reason())
		}
		return e.failJob(storeCtx, job, msg)
	}

	// Update job status to running
	job.Status = types.JobStatusRunning
	if err := e.jobStore.Update(storeCtx, job); err != nil {
		log.Printf("github-rag-ingest: failed to mark job %s running: %v", job.ID, err)
	}

	// Load repository config
	repo, err := e.repoStore.Get(ctx, job.RepoID)
	if err != nil {
		return stop(fmt.Sprintf("failed to load repository: %v", err))
	}

	// Get authentication secret
	var secret *storage.Secret
	if repo.AuthSecretRef != "" {
		secret, err = e.secretStore.GetByRef(ctx, repo.AuthSecretRef)
		if err != nil {
			return stop(fmt.Sprintf("failed to load secret: %v", err))
		}
	}

	// Clone or fetch repository
	e.logJob(ctx, job, types.LogLevelInfo, "Cloning/fetching repository", repo.URL)
	gitRepo, err := e.gitClient.CloneOrFetch(ctx, repo, secret)
	if err != nil {
		return stop(fmt.Sprintf("failed to clone/fetch: %v", err))
	}

	// Get current HEAD commit
	headCommit, err := e.gitClient.GetHead(gitRepo, repo.Branch)
	if err != nil {
		return stop(fmt.Sprintf("failed to get HEAD: %v", err))
	}
	job.ToCommit = headCommit

	// Determine files to process
	var filesToProcess []string
	var filesToDelete []string

	if job.Type == types.JobTypeIncremental && repo.LastSyncCommit != "" {
		// Incremental sync: compute diff
		job.FromCommit = repo.LastSyncCommit
		e.logJob(ctx, job, types.LogLevelInfo, "Computing diff", fmt.Sprintf("from %s to %s", repo.LastSyncCommit, headCommit))

		changes, err := git.ComputeDiff(gitRepo, repo.LastSyncCommit, headCommit)
		if err != nil {
			return stop(fmt.Sprintf("failed to compute diff: %v", err))
		}

		for _, change := range changes {
			switch change.Status {
			case "added", "modified":
				filesToProcess = append(filesToProcess, change.Path)
			case "deleted":
				filesToDelete = append(filesToDelete, change.Path)
			case "renamed":
				filesToDelete = append(filesToDelete, change.OldPath)
				filesToProcess = append(filesToProcess, change.Path)
			}
		}

		e.logJob(ctx, job, types.LogLevelInfo, "Diff computed", fmt.Sprintf("%d files to process, %d files to delete", len(filesToProcess), len(filesToDelete)))
	} else {
		// Full sync: process all files
		e.logJob(ctx, job, types.LogLevelInfo, "Full sync", "listing all files")
		allFiles, err := git.ListAllFiles(gitRepo, headCommit)
		if err != nil {
			return stop(fmt.Sprintf("failed to list files: %v", err))
		}
		filesToProcess = allFiles
	}

	job.Stats.FilesScanned = len(filesToProcess)

	// Process files if not dry-run
	if !job.DryRun {
		err = e.processFiles(ctx, job, repo, gitRepo, headCommit, filesToProcess)
		if err != nil {
			return stop(fmt.Sprintf("failed to process files: %v", err))
		}

		// Delete old chunks for deleted files
		if len(filesToDelete) > 0 {
			err = e.deleteChunksForFiles(ctx, job, repo, filesToDelete)
			if err != nil {
				e.logJob(storeCtx, job, types.LogLevelWarn, "Failed to delete some chunks", err.Error())
			}
		}
		if ctx.Err() != nil {
			// The sync is incomplete: keep the last-synced commit
			return e.cancelJob(storeCtx, job, reason())
		}

		// Update repository last sync
		repo.LastSyncCommit = headCommit
		repo.LastSyncAt = time.Now()
		if err := e.repoStore.Update(storeCtx, repo); err != nil {
			e.logJob(storeCtx, job, types.LogLevelWarn, "Failed to record the synced commit", err.Error())
		}
	} else {
		e.logJob(ctx, job, types.LogLevelInfo, "Dry-run mode", "no changes written to datasource")
	}

	// Complete job
	job.Complete(types.JobStatusSuccess, "")
	if err := e.jobStore.Update(storeCtx, job); err != nil {
		log.Printf("github-rag-ingest: failed to record job %s as completed: %v", job.ID, err)
	}
	e.logJob(storeCtx, job, types.LogLevelInfo, "Job completed successfully", fmt.Sprintf("Processed %d files, %d chunks written", job.Stats.FilesAdded+job.Stats.FilesChanged, job.Stats.ChunksWritten))

	return nil
}

// processFiles processes a list of files and stores chunks
func (e *Engine) processFiles(ctx context.Context, job *types.Job, repo *types.Repository, gitRepo *gogit.Repository, commitSHA string, files []string) error {
	processor := NewFileProcessor(repo, gitRepo, commitSHA)

	var allDocuments []*mgmtpb.DocumentChunk
	batchSize := 50 // Process in batches to avoid memory issues

	for i, filePath := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Process file
		chunks, err := processor.ProcessFile(ctx, gitRepo, commitSHA, filePath)
		if err != nil {
			if IsSkipError(err) {
				job.Stats.FilesSkipped++
				e.logJob(ctx, job, types.LogLevelDebug, "Skipped file", fmt.Sprintf("%s: %v", filePath, err))
				continue
			}
			job.Stats.Errors++
			e.logJob(ctx, job, types.LogLevelError, "Error processing file", fmt.Sprintf("%s: %v", filePath, err))
			continue
		}

		// Log if file produced chunks
		e.logJob(ctx, job, types.LogLevelDebug, "Processing file", fmt.Sprintf("%s: %d chunks", filePath, len(chunks)))

		// Convert to proto format
		for _, chunk := range chunks {
			allDocuments = append(allDocuments, &mgmtpb.DocumentChunk{
				Content:  chunk.Content,
				Metadata: chunk.Metadata.ToMap(),
			})
		}

		job.Stats.FilesAdded++

		// Process batch if we've accumulated enough
		if len(allDocuments) >= batchSize || i == len(files)-1 {
			if len(allDocuments) > 0 {
				e.logJob(ctx, job, types.LogLevelInfo, "Storing batch", fmt.Sprintf("%d chunks to datasource %d", len(allDocuments), repo.DatasourceID))
				err := e.storeBatch(ctx, job, repo, allDocuments)
				if err != nil {
					e.logJob(ctx, job, types.LogLevelError, "Batch storage failed", fmt.Sprintf("%v", err))
					return fmt.Errorf("failed to store batch: %w", err)
				}
				e.logJob(ctx, job, types.LogLevelInfo, "Batch stored", fmt.Sprintf("%d chunks written successfully", len(allDocuments)))
				job.Stats.ChunksWritten += len(allDocuments)
				allDocuments = allDocuments[:0] // Clear batch
			}
		}

		// Log progress every 10 files
		if (i+1)%10 == 0 {
			e.logJob(ctx, job, types.LogLevelInfo, "Progress", fmt.Sprintf("Processed %d/%d files, %d chunks accumulated", i+1, len(files), job.Stats.ChunksWritten))
		}
	}

	return nil
}

// storeBatch stores a batch of document chunks
func (e *Engine) storeBatch(ctx context.Context, job *types.Job, repo *types.Repository, documents []*mgmtpb.DocumentChunk) error {
	e.logJob(ctx, job, types.LogLevelInfo, "Calling ProcessAndStoreDocuments",
		fmt.Sprintf("datasource_id=%d, chunks=%d, first_chunk_preview=%s",
			repo.DatasourceID, len(documents),
			documents[0].Content[:min(50, len(documents[0].Content))]))

	resp, err := e.chunks.ProcessAndStoreDocuments(ctx, repo.DatasourceID, documents)
	if err != nil {
		e.logJob(ctx, job, types.LogLevelError, "ProcessAndStoreDocuments gRPC error", fmt.Sprintf("%v", err))
		return fmt.Errorf("failed to call ProcessAndStoreDocuments: %w", err)
	}

	if !resp.Success {
		e.logJob(ctx, job, types.LogLevelError, "ProcessAndStoreDocuments returned failure", resp.ErrorMessage)
		return fmt.Errorf("ProcessAndStoreDocuments failed: %s", resp.ErrorMessage)
	}

	e.logJob(ctx, job, types.LogLevelInfo, "ProcessAndStoreDocuments succeeded",
		fmt.Sprintf("processed_count=%d", resp.ProcessedCount))

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// deleteChunksForFiles deletes chunks for deleted files
func (e *Engine) deleteChunksForFiles(ctx context.Context, job *types.Job, repo *types.Repository, filePaths []string) error {
	for _, filePath := range filePaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		metadata := map[string]string{
			"repo_id":   repo.ID,
			"file_path": filePath,
		}

		count, err := e.chunks.DeleteDocumentsByMetadata(ctx, repo.DatasourceID, metadata, "AND", false)
		if err != nil {
			e.logJob(ctx, job, types.LogLevelError, "Failed to delete chunks", fmt.Sprintf("file: %s, error: %v", filePath, err))
			continue
		}

		job.Stats.ChunksDeleted += int(count)
		job.Stats.FilesDeleted++
		e.logJob(ctx, job, types.LogLevelInfo, "Deleted chunks", fmt.Sprintf("file: %s, chunks: %d", filePath, count))
	}

	return nil
}

// failJob marks a job as failed
func (e *Engine) failJob(ctx context.Context, job *types.Job, errorMessage string) error {
	job.Complete(types.JobStatusFailed, errorMessage)
	if err := e.jobStore.Update(ctx, job); err != nil {
		log.Printf("github-rag-ingest: failed to record job %s as failed: %v", job.ID, err)
	}
	e.logJob(ctx, job, types.LogLevelError, "Job failed", errorMessage)
	return fmt.Errorf("job failed: %s", errorMessage)
}

// cancelJob marks a job as cancelled
func (e *Engine) cancelJob(ctx context.Context, job *types.Job, reason string) error {
	job.Complete(types.JobStatusCancelled, reason)
	if err := e.jobStore.Update(ctx, job); err != nil {
		log.Printf("github-rag-ingest: failed to record job %s as cancelled: %v", job.ID, err)
	}
	e.logJob(ctx, job, types.LogLevelWarn, "Job cancelled",
		fmt.Sprintf("Stopped after %d files, %d chunks written; the last synced commit is unchanged", job.Stats.FilesAdded+job.Stats.FilesChanged, job.Stats.ChunksWritten))
	return ErrJobCancelled
}

// logJob adds a log entry to the job. Logs are written even once the job is
// cancelled, so its log explains where it stopped.
func (e *Engine) logJob(ctx context.Context, job *types.Job, level, message, details string) {
	ctx = context.WithoutCancel(ctx)
	entry := &types.JobLog{
		JobID:     job.ID,
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Details:   details,
	}
	if err := e.jobStore.AddLog(ctx, entry); err != nil {
		log.Printf("github-rag-ingest: failed to record %s log for job %s: %v", level, job.ID, err)
	}
}
