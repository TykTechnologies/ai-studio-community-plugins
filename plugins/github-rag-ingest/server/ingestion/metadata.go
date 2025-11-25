package ingestion

import (
	"path/filepath"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/chunking"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// MetadataBuilder builds metadata for chunks
type MetadataBuilder struct {
	repo      *types.Repository
	commitSHA string
}

// NewMetadataBuilder creates a new metadata builder
func NewMetadataBuilder(repo *types.Repository, commitSHA string) *MetadataBuilder {
	return &MetadataBuilder{
		repo:      repo,
		commitSHA: commitSHA,
	}
}

// BuildForChunk builds metadata for a single chunk
func (mb *MetadataBuilder) BuildForChunk(filePath string, chunk *types.Chunk, chunkIndex, totalChunks int) *types.ChunkMetadata {
	fileName := filepath.Base(filePath)
	fileType := chunking.DetectLanguage(filePath)

	githubURL := types.BuildGitHubURL(
		mb.repo.Owner,
		mb.repo.Name,
		mb.commitSHA,
		filePath,
		chunk.LineStart,
		chunk.LineEnd,
	)

	return &types.ChunkMetadata{
		Source:             "github-rag-ingest",
		RepoID:             mb.repo.ID,
		RepoName:           mb.repo.Name,
		RepoOwner:          mb.repo.Owner,
		RepoHost:           mb.repo.Host,
		Branch:             mb.repo.Branch,
		CommitSHA:          mb.commitSHA,
		FilePath:           filePath,
		FileName:           fileName,
		FileType:           fileType,
		ChunkIndex:         chunkIndex,
		TotalChunks:        totalChunks,
		LineStart:          chunk.LineStart,
		LineEnd:            chunk.LineEnd,
		GitHubURL:          githubURL,
		IngestionTimestamp: time.Now(),
		Namespace:          mb.repo.Namespace,
	}
}
