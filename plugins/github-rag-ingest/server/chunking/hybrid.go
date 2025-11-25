package chunking

import (
	"path/filepath"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// HybridChunker selects the appropriate chunking strategy based on file type
type HybridChunker struct {
	chunkSize    int
	chunkOverlap int
	simple       *SimpleChunker
	codeAware    *CodeAwareChunker
}

// NewHybridChunker creates a new hybrid chunker
func NewHybridChunker(chunkSize, chunkOverlap int) *HybridChunker {
	return &HybridChunker{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		simple:       NewSimpleChunker(chunkSize, chunkOverlap),
		codeAware:    NewCodeAwareChunker(chunkSize, chunkOverlap),
	}
}

// Chunk selects the appropriate chunker based on file type
func (c *HybridChunker) Chunk(content []byte, filePath, fileType string) ([]types.Chunk, error) {
	category := categorizeFile(filePath, fileType)

	switch category {
	case "code":
		// Use code-aware chunking for source code
		return c.codeAware.Chunk(content, filePath, fileType)
	case "documentation":
		// Use markdown-aware chunking for docs
		return c.chunkMarkdown(content, filePath)
	case "config":
		// Use simple chunking for config files
		return c.simple.Chunk(content, filePath, fileType)
	default:
		// Default to simple chunking
		return c.simple.Chunk(content, filePath, fileType)
	}
}

// categorizeFile determines the file category based on path and extension
func categorizeFile(filePath, fileType string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath))

	// Code files
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".rs": true, ".rb": true, ".php": true, ".cs": true, ".swift": true,
	}
	if codeExts[ext] {
		return "code"
	}

	// Documentation files
	docExts := map[string]bool{
		".md": true, ".mdx": true, ".rst": true, ".txt": true, ".adoc": true,
	}
	if docExts[ext] || baseName == "readme" {
		return "documentation"
	}

	// Config files
	configExts := map[string]bool{
		".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
		".xml": true, ".conf": true, ".config": true,
	}
	if configExts[ext] {
		return "config"
	}

	return "other"
}

// chunkMarkdown chunks markdown by headings
func (c *HybridChunker) chunkMarkdown(content []byte, filePath string) ([]types.Chunk, error) {
	text := string(content)
	lines := strings.Split(text, "\n")

	var chunks []types.Chunk
	currentChunk := ""
	currentLineStart := 1
	currentLine := 1

	for _, line := range lines {
		// Check for heading (starts with #)
		isHeading := strings.HasPrefix(strings.TrimSpace(line), "#")

		// Start new chunk on headings if current chunk is not empty
		if isHeading && currentChunk != "" && len(currentChunk) > 100 {
			chunks = append(chunks, types.Chunk{
				Content:   strings.TrimSpace(currentChunk),
				LineStart: currentLineStart,
				LineEnd:   currentLine - 1,
			})
			currentChunk = ""
			currentLineStart = currentLine
		}

		// Check if adding this line would exceed chunk size
		if len(currentChunk)+len(line)+1 > c.chunkSize && currentChunk != "" {
			chunks = append(chunks, types.Chunk{
				Content:   strings.TrimSpace(currentChunk),
				LineStart: currentLineStart,
				LineEnd:   currentLine - 1,
			})

			// Start new chunk
			currentChunk = ""
			currentLineStart = currentLine
		}

		// Add line to current chunk
		if currentChunk != "" {
			currentChunk += "\n"
		}
		currentChunk += line
		currentLine++
	}

	// Add final chunk
	if currentChunk != "" {
		chunks = append(chunks, types.Chunk{
			Content:   strings.TrimSpace(currentChunk),
			LineStart: currentLineStart,
			LineEnd:   currentLine - 1,
		})
	}

	return chunks, nil
}
