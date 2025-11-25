package types

import (
	"fmt"
	"time"
)

// Chunk represents a text chunk with metadata for RAG storage
type Chunk struct {
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata"`
	LineStart int               `json:"line_start"`
	LineEnd   int               `json:"line_end"`
}

// ChunkMetadata builds metadata for a chunk
type ChunkMetadata struct {
	Source             string
	RepoID             string
	RepoName           string
	RepoOwner          string
	RepoHost           string
	Branch             string
	CommitSHA          string
	FilePath           string
	FileName           string
	FileType           string
	ChunkIndex         int
	TotalChunks        int
	LineStart          int
	LineEnd            int
	GitHubURL          string
	IngestionTimestamp time.Time
	Namespace          string
}

// ToMap converts ChunkMetadata to a map for storage
func (cm *ChunkMetadata) ToMap() map[string]string {
	return map[string]string{
		"source":               cm.Source,
		"repo_id":              cm.RepoID,
		"repo_name":            cm.RepoName,
		"repo_owner":           cm.RepoOwner,
		"repo_host":            cm.RepoHost,
		"branch":               cm.Branch,
		"commit_sha":           cm.CommitSHA,
		"file_path":            cm.FilePath,
		"file_name":            cm.FileName,
		"file_type":            cm.FileType,
		"chunk_index":          string(rune(cm.ChunkIndex)),
		"total_chunks":         string(rune(cm.TotalChunks)),
		"line_start":           string(rune(cm.LineStart)),
		"line_end":             string(rune(cm.LineEnd)),
		"github_url":           cm.GitHubURL,
		"ingestion_timestamp": cm.IngestionTimestamp.Format(time.RFC3339),
		"namespace":            cm.Namespace,
	}
}

// BuildGitHubURL constructs the GitHub URL for a file and line range
func BuildGitHubURL(repoOwner, repoName, commitSHA, filePath string, lineStart, lineEnd int) string {
	if lineStart == lineEnd {
		return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d",
			repoOwner, repoName, commitSHA, filePath, lineStart)
	}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d-L%d",
		repoOwner, repoName, commitSHA, filePath, lineStart, lineEnd)
}
