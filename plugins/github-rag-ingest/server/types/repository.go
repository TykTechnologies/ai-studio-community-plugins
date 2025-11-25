package types

import "time"

// Repository represents a Git repository configuration for RAG ingestion
type Repository struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Owner           string    `json:"owner"`
	Host            string    `json:"host"`
	URL             string    `json:"url"`
	Branch          string    `json:"branch"`
	AuthType        string    `json:"auth_type"` // pat, ssh, public
	AuthSecretRef   string    `json:"auth_secret_ref"`
	DatasourceID    uint32    `json:"datasource_id"`
	Namespace       string    `json:"namespace"`
	TargetPaths     []string  `json:"target_paths"`
	FileMasks       []string  `json:"file_masks"`
	IgnorePatterns  []string  `json:"ignore_patterns"`
	UseGitignore    bool      `json:"use_gitignore"`
	UseRagignore    bool      `json:"use_ragignore"`
	MaxFileSizeKB   int       `json:"max_file_size_kb"`
	ChunkingStrategy string   `json:"chunking_strategy"` // simple, code_aware, hybrid
	ChunkSize       int       `json:"chunk_size"`
	ChunkOverlap    int       `json:"chunk_overlap"`
	SyncSchedule    string    `json:"sync_schedule"` // cron expression
	SyncEnabled     bool      `json:"sync_enabled"`
	LastSyncCommit  string    `json:"last_sync_commit"`
	LastSyncAt      time.Time `json:"last_sync_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AuthType constants
const (
	AuthTypePAT    = "pat"
	AuthTypeSSH    = "ssh"
	AuthTypePublic = "public"
)

// ChunkingStrategy constants
const (
	ChunkingStrategySimple    = "simple"
	ChunkingStrategyCodeAware = "code_aware"
	ChunkingStrategyHybrid    = "hybrid"
)

// NewRepository creates a new repository with defaults
func NewRepository(name, url, branch string) *Repository {
	return &Repository{
		Name:             name,
		URL:              url,
		Branch:           branch,
		Host:             "github.com",
		AuthType:         AuthTypePublic,
		TargetPaths:      []string{},
		FileMasks:        []string{"*"},
		IgnorePatterns:   []string{},
		UseGitignore:     true,
		UseRagignore:     true,
		MaxFileSizeKB:    1024,
		ChunkingStrategy: ChunkingStrategyHybrid,
		ChunkSize:        1000,
		ChunkOverlap:     200,
		SyncEnabled:      false,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
}

// Validate checks if the repository configuration is valid
func (r *Repository) Validate() error {
	if r.Name == "" {
		return ErrRepositoryNameRequired
	}
	if r.URL == "" {
		return ErrRepositoryURLRequired
	}
	if r.Branch == "" {
		return ErrRepositoryBranchRequired
	}
	if r.DatasourceID == 0 {
		return ErrRepositoryDatasourceRequired
	}
	if r.ChunkSize <= 0 {
		return ErrInvalidChunkSize
	}
	if r.ChunkOverlap < 0 || r.ChunkOverlap >= r.ChunkSize {
		return ErrInvalidChunkOverlap
	}
	return nil
}
