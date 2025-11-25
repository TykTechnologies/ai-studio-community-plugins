package ingestion

import (
	"context"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/chunking"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/git"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	gogit "github.com/go-git/go-git/v5"
)

// FileProcessor processes individual files for ingestion
type FileProcessor struct {
	repo           *types.Repository
	chunker        chunking.Chunker
	filter         *FileFilter
	metadataBuilder *MetadataBuilder
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(repo *types.Repository, gitRepo *gogit.Repository, commitSHA string) *FileProcessor {
	chunkConfig := &chunking.ChunkConfig{
		Strategy:     repo.ChunkingStrategy,
		ChunkSize:    repo.ChunkSize,
		ChunkOverlap: repo.ChunkOverlap,
	}

	return &FileProcessor{
		repo:            repo,
		chunker:         chunking.NewChunker(chunkConfig),
		filter:          NewFileFilter(repo, gitRepo, commitSHA),
		metadataBuilder: NewMetadataBuilder(repo, commitSHA),
	}
}

// ProcessFile processes a single file and returns chunks with metadata
func (fp *FileProcessor) ProcessFile(ctx context.Context, gitRepo *gogit.Repository, commitSHA, filePath string) ([]*ProcessedChunk, error) {
	// Read file content
	content, err := git.ReadFile(gitRepo, commitSHA, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Check if file should be included
	include, reason := fp.filter.ShouldInclude(filePath, content)
	if !include {
		return nil, &SkipError{Reason: reason}
	}

	// Chunk the file
	fileType := chunking.DetectLanguage(filePath)
	chunks, err := fp.chunker.Chunk(content, filePath, fileType)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk file: %w", err)
	}

	// Build metadata for each chunk (pass file content for char offset calculation)
	fileContentStr := string(content)
	processedChunks := make([]*ProcessedChunk, len(chunks))
	for i, chunk := range chunks {
		metadata := fp.metadataBuilder.BuildForChunk(filePath, &chunk, i, len(chunks), fileContentStr)
		processedChunks[i] = &ProcessedChunk{
			Content:  chunk.Content,
			Metadata: metadata,
		}
	}

	return processedChunks, nil
}

// ProcessedChunk represents a chunk with its metadata ready for storage
type ProcessedChunk struct {
	Content  string
	Metadata *types.ChunkMetadata
}

// SkipError indicates a file should be skipped
type SkipError struct {
	Reason string
}

func (e *SkipError) Error() string {
	return fmt.Sprintf("file skipped: %s", e.Reason)
}

// IsSkipError checks if an error is a SkipError
func IsSkipError(err error) bool {
	_, ok := err.(*SkipError)
	return ok
}
