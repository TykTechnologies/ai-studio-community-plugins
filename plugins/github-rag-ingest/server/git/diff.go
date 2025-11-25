package git

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// FileChange represents a file change in a diff
type FileChange struct {
	Path   string
	Status string // "added", "modified", "deleted", "renamed"
	OldPath string // For renames
}

// ComputeDiff computes the diff between two commits
func ComputeDiff(gitRepo *git.Repository, fromCommitSHA, toCommitSHA string) ([]FileChange, error) {
	// Get commit objects
	fromCommit, err := gitRepo.CommitObject(plumbing.NewHash(fromCommitSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get from commit: %w", err)
	}

	toCommit, err := gitRepo.CommitObject(plumbing.NewHash(toCommitSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get to commit: %w", err)
	}

	// Get trees
	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get from tree: %w", err)
	}

	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get to tree: %w", err)
	}

	// Compute diff
	changes, err := fromTree.Diff(toTree)
	if err != nil {
		return nil, fmt.Errorf("failed to compute diff: %w", err)
	}

	// Convert to FileChange
	fileChanges := make([]FileChange, 0, len(changes))
	for _, change := range changes {
		from, to, err := change.Files()
		if err != nil {
			continue // Skip errors
		}

		fc := FileChange{}

		if from == nil && to != nil {
			// File added
			fc.Path = to.Name
			fc.Status = "added"
		} else if from != nil && to == nil {
			// File deleted
			fc.Path = from.Name
			fc.Status = "deleted"
		} else if from != nil && to != nil {
			if from.Name != to.Name {
				// File renamed
				fc.Path = to.Name
				fc.OldPath = from.Name
				fc.Status = "renamed"
			} else {
				// File modified
				fc.Path = to.Name
				fc.Status = "modified"
			}
		}

		fileChanges = append(fileChanges, fc)
	}

	return fileChanges, nil
}

// ListAllFiles lists all files in a commit
func ListAllFiles(gitRepo *git.Repository, commitSHA string) ([]string, error) {
	commit, err := gitRepo.CommitObject(plumbing.NewHash(commitSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree: %w", err)
	}

	var files []string
	err = tree.Files().ForEach(func(f *object.File) error {
		files = append(files, f.Name)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate files: %w", err)
	}

	return files, nil
}

// ReadFile reads file content at a specific commit
func ReadFile(gitRepo *git.Repository, commitSHA, filePath string) ([]byte, error) {
	commit, err := gitRepo.CommitObject(plumbing.NewHash(commitSHA))
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	file, err := commit.File(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("failed to read file contents: %w", err)
	}

	return []byte(content), nil
}
