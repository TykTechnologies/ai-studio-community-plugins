package chunking

import "github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"

// Chunker interface defines the contract for text chunking strategies
type Chunker interface {
	// Chunk splits content into chunks with line number tracking
	Chunk(content []byte, filePath, fileType string) ([]types.Chunk, error)
}

// ChunkConfig holds configuration for chunking
type ChunkConfig struct {
	Strategy     string // simple, code_aware, hybrid
	ChunkSize    int    // Maximum chunk size in characters
	ChunkOverlap int    // Overlap between chunks in characters
}

// NewChunker creates a chunker based on strategy
func NewChunker(config *ChunkConfig) Chunker {
	switch config.Strategy {
	case types.ChunkingStrategySimple:
		return NewSimpleChunker(config.ChunkSize, config.ChunkOverlap)
	case types.ChunkingStrategyCodeAware:
		return NewCodeAwareChunker(config.ChunkSize, config.ChunkOverlap)
	case types.ChunkingStrategyHybrid:
		return NewHybridChunker(config.ChunkSize, config.ChunkOverlap)
	default:
		// Default to hybrid
		return NewHybridChunker(config.ChunkSize, config.ChunkOverlap)
	}
}
