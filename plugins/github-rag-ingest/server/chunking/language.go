package chunking

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DetectLanguage detects the programming language from file extension
func DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	langMap := map[string]string{
		".go":    "go",
		".py":    "python",
		".js":    "javascript",
		".ts":    "typescript",
		".tsx":   "typescript",
		".jsx":   "javascript",
		".java":  "java",
		".c":     "c",
		".cpp":   "cpp",
		".cc":    "cpp",
		".cxx":   "cpp",
		".h":     "c",
		".hpp":   "cpp",
		".rs":    "rust",
		".rb":    "ruby",
		".php":   "php",
		".cs":    "csharp",
		".swift": "swift",
		".kt":    "kotlin",
		".scala": "scala",
		".sh":    "bash",
		".bash":  "bash",
		".zsh":   "bash",
		".ps1":   "powershell",
		".r":     "r",
		".sql":   "sql",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}

	return "unknown"
}

// IsCodeFile returns true if the file is a source code file
func IsCodeFile(filePath string) bool {
	lang := DetectLanguage(filePath)
	return lang != "unknown"
}

// IsBinaryFile attempts to detect if a file is binary based on content
func IsBinaryFile(content []byte) bool {
	checkSize := 8192
	if len(content) < checkSize {
		checkSize = len(content)
	}
	if checkSize == 0 {
		return false
	}

	sample := content[:checkSize]

	// Heuristic 1: null bytes indicate binary
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}

	// Heuristic 2: high ratio of invalid UTF-8 sequences suggests binary.
	// A few invalid bytes may just be a text file with encoding issues (e.g. Latin-1),
	// but many invalid bytes indicate a truly binary file.
	if !utf8.Valid(sample) {
		invalidCount := 0
		for i := 0; i < len(sample); {
			_, size := utf8.DecodeRune(sample[i:])
			if size == 1 && sample[i] >= 0x80 {
				invalidCount++
			}
			i += size
		}
		if float64(invalidCount)/float64(checkSize) > 0.10 {
			return true
		}
	}

	return false
}
