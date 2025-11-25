package git

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheManager manages the local git repository cache
type CacheManager struct {
	cacheDir    string
	maxAgeHours int
}

// NewCacheManager creates a new cache manager
func NewCacheManager(cacheDir string, maxAgeHours int) *CacheManager {
	return &CacheManager{
		cacheDir:    cacheDir,
		maxAgeHours: maxAgeHours,
	}
}

// CleanupOld removes cached repositories older than maxAgeHours
func (cm *CacheManager) CleanupOld() (int, error) {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read cache directory: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(cm.maxAgeHours) * time.Hour)
	cleaned := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		repoPath := filepath.Join(cm.cacheDir, entry.Name())
		info, err := os.Stat(repoPath)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(repoPath); err == nil {
				cleaned++
			}
		}
	}

	return cleaned, nil
}

// GetSize returns the total size of the cache directory in bytes
func (cm *CacheManager) GetSize() (int64, error) {
	var size int64

	err := filepath.Walk(cm.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// GetRepoCount returns the number of cached repositories
func (cm *CacheManager) GetRepoCount() (int, error) {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}

	return count, nil
}
