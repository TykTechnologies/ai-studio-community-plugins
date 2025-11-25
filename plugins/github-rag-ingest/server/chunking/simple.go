package chunking

import (
	"strings"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// SimpleChunker implements simple fixed-size text chunking with overlap
type SimpleChunker struct {
	chunkSize    int
	chunkOverlap int
}

// NewSimpleChunker creates a new simple chunker
func NewSimpleChunker(chunkSize, chunkOverlap int) *SimpleChunker {
	return &SimpleChunker{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}
}

// Chunk splits content into fixed-size chunks with overlap
func (c *SimpleChunker) Chunk(content []byte, filePath, fileType string) ([]types.Chunk, error) {
	text := string(content)
	lines := strings.Split(text, "\n")

	var chunks []types.Chunk
	currentChunk := ""
	currentLineStart := 1
	currentLine := 1

	for _, line := range lines {
		// Check if adding this line would exceed chunk size
		if len(currentChunk)+len(line)+1 > c.chunkSize && currentChunk != "" {
			// Save current chunk
			chunks = append(chunks, types.Chunk{
				Content:   strings.TrimSpace(currentChunk),
				LineStart: currentLineStart,
				LineEnd:   currentLine - 1,
			})

			// Start new chunk with overlap
			if c.chunkOverlap > 0 {
				overlapText := getOverlap(currentChunk, c.chunkOverlap)
				overlapLines := countLines(overlapText)
				currentChunk = overlapText
				currentLineStart = currentLine - overlapLines
			} else {
				currentChunk = ""
				currentLineStart = currentLine
			}
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

// getOverlap returns the last N characters from text
func getOverlap(text string, size int) string {
	if len(text) <= size {
		return text
	}
	return text[len(text)-size:]
}

// countLines counts the number of newlines in text
func countLines(text string) int {
	return strings.Count(text, "\n") + 1
}
