package chunking

import (
	"path/filepath"
	"strings"
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
	// Simple heuristic: if file contains null bytes in first 8KB, it's likely binary
	checkSize := 8192
	if len(content) < checkSize {
		checkSize = len(content)
	}

	for i := 0; i < checkSize; i++ {
		if content[i] == 0 {
			return true
		}
	}

	return false
}
