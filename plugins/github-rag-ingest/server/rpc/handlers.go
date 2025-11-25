package rpc

import (
	"encoding/json"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/git"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/ingestion"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/secrets"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
)

// Handler manages RPC handlers with dependencies
type Handler struct {
	repoStore     *storage.RepositoryStore
	jobStore      *storage.JobStore
	secretStore   *storage.SecretStore
	gitClient     *git.Client
	secretBackend secrets.Backend
	engine        *ingestion.Engine
}

// NewHandler creates a new RPC handler
func NewHandler(
	repoStore *storage.RepositoryStore,
	jobStore *storage.JobStore,
	secretStore *storage.SecretStore,
	gitClient *git.Client,
	secretBackend secrets.Backend,
	engine *ingestion.Engine,
) *Handler {
	return &Handler{
		repoStore:     repoStore,
		jobStore:      jobStore,
		secretStore:   secretStore,
		gitClient:     gitClient,
		secretBackend: secretBackend,
		engine:        engine,
	}
}

// Response helpers

func successResponse(data interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

func errorResponse(message string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// Request/Response types

type CreateRepositoryRequest struct {
	Name             string   `json:"name"`
	Owner            string   `json:"owner"`
	URL              string   `json:"url"`
	Branch           string   `json:"branch"`
	AuthType         string   `json:"auth_type"`
	PATToken         string   `json:"pat_token,omitempty"`
	SSHPrivateKey    string   `json:"ssh_private_key,omitempty"`
	SSHPassphrase    string   `json:"ssh_passphrase,omitempty"`
	DatasourceID     uint32   `json:"datasource_id"`
	TargetPaths      []string `json:"target_paths"`
	FileMasks        []string `json:"file_masks"`
	IgnorePatterns   []string `json:"ignore_patterns"`
	ChunkingStrategy string   `json:"chunking_strategy"`
	ChunkSize        int      `json:"chunk_size"`
	ChunkOverlap     int      `json:"chunk_overlap"`
	SyncSchedule     string   `json:"sync_schedule,omitempty"`
	SyncEnabled      bool     `json:"sync_enabled"`
}

type TriggerIngestionRequest struct {
	RepoID string `json:"repo_id"`
	Type   string `json:"type"`   // full, incremental
	DryRun bool   `json:"dry_run"`
}

type GetJobLogsRequest struct {
	JobID  string `json:"job_id"`
	Level  string `json:"level,omitempty"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// Public accessor methods for scheduler

func (h *Handler) GetRepoStore() *storage.RepositoryStore {
	return h.repoStore
}

func (h *Handler) GetJobStore() *storage.JobStore {
	return h.jobStore
}

func (h *Handler) GetEngine() *ingestion.Engine {
	return h.engine
}
