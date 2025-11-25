package ingestion

import (
	"path/filepath"
	"strings"
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
func (mb *MetadataBuilder) BuildForChunk(filePath string, chunk *types.Chunk, chunkIndex, totalChunks int, fileContent string) *types.ChunkMetadata {
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

	// Extract title from chunk content (first heading or filename)
	title := extractTitle(chunk.Content, fileName)

	// Calculate character offsets (approximate from line numbers)
	charStart, charEnd := calculateCharOffsets(fileContent, chunk.LineStart, chunk.LineEnd)

	return &types.ChunkMetadata{
		// AI Studio standard fields
		Content:            chunk.Content,
		Title:              title,
		CharStart:          charStart,
		CharEnd:            charEnd,

		// GitHub-specific fields
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

// extractTitle extracts a title from chunk content (first heading or filename)
func extractTitle(content, fallback string) string {
	lines := strings.Split(content, "\n")

	// Look for markdown heading in first few lines
	for i := 0; i < min(5, len(lines)); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
		if strings.HasPrefix(line, "## ") {
			return strings.TrimPrefix(line, "## ")
		}
	}

	// Fallback to filename
	return fallback
}

// calculateCharOffsets calculates character offsets from line numbers
func calculateCharOffsets(fileContent string, lineStart, lineEnd int) (int, int) {
	lines := strings.Split(fileContent, "\n")

	charStart := 0
	for i := 0; i < lineStart-1 && i < len(lines); i++ {
		charStart += len(lines[i]) + 1 // +1 for newline
	}

	charEnd := charStart
	for i := lineStart - 1; i < lineEnd && i < len(lines); i++ {
		charEnd += len(lines[i]) + 1
	}

	return charStart, charEnd
}
