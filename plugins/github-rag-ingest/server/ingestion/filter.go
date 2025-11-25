package ingestion

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/chunking"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/bmatcuk/doublestar/v4"
	gitignore "github.com/sabhiram/go-gitignore"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// FileFilter handles file filtering based on ignore patterns and rules
type FileFilter struct {
	repo            *types.Repository
	gitignoreMatcher *gitignore.GitIgnore
	ragignoreMatcher *gitignore.GitIgnore
}

// NewFileFilter creates a new file filter with gitignore/ragignore support
func NewFileFilter(repo *types.Repository, gitRepo *gogit.Repository, commitSHA string) *FileFilter {
	filter := &FileFilter{repo: repo}

	// Load .gitignore if enabled
	if repo.UseGitignore {
		if gitignoreContent, err := readFileFromRepo(gitRepo, commitSHA, ".gitignore"); err == nil {
			lines := strings.Split(string(gitignoreContent), "\n")
			filter.gitignoreMatcher = gitignore.CompileIgnoreLines(lines...)
		}
	}

	// Load .ragignore if enabled
	if repo.UseRagignore {
		if ragignoreContent, err := readFileFromRepo(gitRepo, commitSHA, ".ragignore"); err == nil {
			lines := strings.Split(string(ragignoreContent), "\n")
			filter.ragignoreMatcher = gitignore.CompileIgnoreLines(lines...)
		}
	}

	return filter
}

// readFileFromRepo reads a file from a specific commit
func readFileFromRepo(gitRepo *gogit.Repository, commitSHA, filePath string) ([]byte, error) {
	commit, err := gitRepo.CommitObject(plumbing.NewHash(commitSHA))
	if err != nil {
		return nil, err
	}

	file, err := commit.File(filePath)
	if err != nil {
		return nil, err
	}

	content, err := file.Contents()
	if err != nil {
		return nil, err
	}

	return []byte(content), nil
}

// ShouldInclude determines if a file should be included for ingestion
func (f *FileFilter) ShouldInclude(filePath string, content []byte) (bool, string) {
	// Check .gitignore if enabled and loaded
	if f.gitignoreMatcher != nil && f.gitignoreMatcher.MatchesPath(filePath) {
		return false, "matched by .gitignore"
	}

	// Check .ragignore if enabled and loaded
	if f.ragignoreMatcher != nil && f.ragignoreMatcher.MatchesPath(filePath) {
		return false, "matched by .ragignore"
	}

	// Check file masks (if specified)
	if len(f.repo.FileMasks) > 0 {
		matched := false
		for _, mask := range f.repo.FileMasks {
			if ok, _ := doublestar.Match(mask, filePath); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false, "does not match file masks"
		}
	}

	// Check target paths (if specified)
	if len(f.repo.TargetPaths) > 0 {
		matched := false
		for _, targetPath := range f.repo.TargetPaths {
			if strings.HasPrefix(filePath, targetPath) {
				matched = true
				break
			}
		}
		if !matched {
			return false, "not in target paths"
		}
	}

	// Check custom ignore patterns
	for _, pattern := range f.repo.IgnorePatterns {
		if ok, _ := doublestar.Match(pattern, filePath); ok {
			return false, fmt.Sprintf("matches ignore pattern: %s", pattern)
		}
	}

	// Check default ignore patterns
	defaultIgnores := []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		"*.min.js",
		"*.min.css",
		"*.map",
		"*.lock",
	}
	for _, pattern := range defaultIgnores {
		if ok, _ := doublestar.Match(pattern, filePath); ok {
			return false, fmt.Sprintf("matches default ignore: %s", pattern)
		}
	}

	// Check file size
	if len(content) > f.repo.MaxFileSizeKB*1024 {
		return false, fmt.Sprintf("exceeds max file size: %d KB", f.repo.MaxFileSizeKB)
	}

	// Check if binary
	if chunking.IsBinaryFile(content) {
		return false, "binary file detected"
	}

	return true, ""
}

// Helper to extract file extension
func getExtension(filePath string) string {
	return strings.ToLower(filepath.Ext(filePath))
}
