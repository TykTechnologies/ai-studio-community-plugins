package chunking

import (
	"regexp"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// CodeAwareChunker implements code-aware chunking using regex patterns
// TODO: Enhance with tree-sitter for more accurate AST-based parsing
type CodeAwareChunker struct {
	chunkSize    int
	chunkOverlap int
	simple       *SimpleChunker
}

// NewCodeAwareChunker creates a new code-aware chunker
func NewCodeAwareChunker(chunkSize, chunkOverlap int) *CodeAwareChunker {
	return &CodeAwareChunker{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		simple:       NewSimpleChunker(chunkSize, chunkOverlap),
	}
}

// Chunk splits code into logical units based on language
func (c *CodeAwareChunker) Chunk(content []byte, filePath, fileType string) ([]types.Chunk, error) {
	text := string(content)
	lang := DetectLanguage(filePath)

	switch lang {
	case "go":
		return c.chunkGo(text)
	case "python":
		return c.chunkPython(text)
	case "javascript", "typescript":
		return c.chunkJavaScript(text)
	default:
		// Fall back to simple chunking for unsupported languages
		return c.simple.Chunk(content, filePath, fileType)
	}
}

// chunkGo chunks Go source code by functions and methods
func (c *CodeAwareChunker) chunkGo(text string) ([]types.Chunk, error) {
	// Regex to match Go function/method declarations
	funcPattern := regexp.MustCompile(`(?m)^func\s+(\([^)]+\)\s+)?(\w+)\s*\(`)

	lines := strings.Split(text, "\n")
	matches := funcPattern.FindAllStringIndex(text, -1)

	if len(matches) == 0 {
		// No functions found, use simple chunking
		return c.simple.Chunk([]byte(text), "", "go")
	}

	var chunks []types.Chunk

	// Extract function chunks
	for i, match := range matches {
		startLine := getLineNumber(text, match[0])
		var endLine int

		if i < len(matches)-1 {
			endLine = getLineNumber(text, matches[i+1][0]) - 1
		} else {
			endLine = len(lines)
		}

		if startLine < 1 || endLine > len(lines) {
			continue
		}

		funcLines := lines[startLine-1 : endLine]
		funcText := strings.Join(funcLines, "\n")

		// If function is too large, split it
		if len(funcText) > c.chunkSize {
			subChunks, _ := c.simple.Chunk([]byte(funcText), "", "go")
			for _, sc := range subChunks {
				sc.LineStart += startLine - 1
				sc.LineEnd += startLine - 1
				chunks = append(chunks, sc)
			}
		} else {
			chunks = append(chunks, types.Chunk{
				Content:   strings.TrimSpace(funcText),
				LineStart: startLine,
				LineEnd:   endLine,
			})
		}
	}

	return chunks, nil
}

// chunkPython chunks Python code by functions and classes
func (c *CodeAwareChunker) chunkPython(text string) ([]types.Chunk, error) {
	// Regex for Python def/class at column 0 (no indentation)
	pattern := regexp.MustCompile(`(?m)^(def|class)\s+\w+`)
	return c.chunkByPattern(text, pattern)
}

// chunkJavaScript chunks JavaScript/TypeScript by functions and classes
func (c *CodeAwareChunker) chunkJavaScript(text string) ([]types.Chunk, error) {
	// Regex for function/class in JS/TS
	pattern := regexp.MustCompile(`(?m)^(export\s+)?(function|class|const\s+\w+\s*=\s*(async\s+)?function)\s+`)
	return c.chunkByPattern(text, pattern)
}

// chunkByPattern is a generic pattern-based chunker
func (c *CodeAwareChunker) chunkByPattern(text string, pattern *regexp.Regexp) ([]types.Chunk, error) {
	matches := pattern.FindAllStringIndex(text, -1)

	if len(matches) == 0 {
		return c.simple.Chunk([]byte(text), "", "")
	}

	lines := strings.Split(text, "\n")
	var chunks []types.Chunk

	for i, match := range matches {
		startLine := getLineNumber(text, match[0])
		var endLine int

		if i < len(matches)-1 {
			endLine = getLineNumber(text, matches[i+1][0]) - 1
		} else {
			endLine = len(lines)
		}

		if startLine < 1 || endLine > len(lines) {
			continue
		}

		blockLines := lines[startLine-1 : endLine]
		blockText := strings.Join(blockLines, "\n")

		if len(blockText) > c.chunkSize {
			// Split large blocks
			subChunks, _ := c.simple.Chunk([]byte(blockText), "", "")
			for _, sc := range subChunks {
				sc.LineStart += startLine - 1
				sc.LineEnd += startLine - 1
				chunks = append(chunks, sc)
			}
		} else {
			chunks = append(chunks, types.Chunk{
				Content:   strings.TrimSpace(blockText),
				LineStart: startLine,
				LineEnd:   endLine,
			})
		}
	}

	return chunks, nil
}

// getLineNumber returns the line number for a byte offset in text
func getLineNumber(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	return strings.Count(text[:offset], "\n") + 1
}
