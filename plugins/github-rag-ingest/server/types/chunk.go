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
	// AI Studio standard fields
	Content            string    // Full chunk content (for "text" field)
	Title              string    // Chunk title (extracted or filename)
	CharStart          int       // Character offset start
	CharEnd            int       // Character offset end

	// GitHub-specific fields
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
		// AI Studio chat-compatible fields
		"filename":             cm.FileName,                              // Chat compatibility alias
		"file_name":            cm.FileName,                              // Keep for compatibility
		"title":                cm.Title,                                 // Chunk/file title
		"text":                 cm.Content,                               // Full chunk text
		"start":                fmt.Sprintf("%d", cm.CharStart),          // Character offset
		"end":                  fmt.Sprintf("%d", cm.CharEnd),            // Character offset

		// GitHub-specific fields
		"source":               cm.Source,
		"repo_id":              cm.RepoID,
		"repo_name":            cm.RepoName,
		"repo_owner":           cm.RepoOwner,
		"repo_host":            cm.RepoHost,
		"branch":               cm.Branch,
		"commit_sha":           cm.CommitSHA,
		"file_path":            cm.FilePath,
		"file_type":            cm.FileType,
		"chunk_index":          fmt.Sprintf("%d", cm.ChunkIndex),
		"total_chunks":         fmt.Sprintf("%d", cm.TotalChunks),
		"line_start":           fmt.Sprintf("%d", cm.LineStart),
		"line_end":             fmt.Sprintf("%d", cm.LineEnd),
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
