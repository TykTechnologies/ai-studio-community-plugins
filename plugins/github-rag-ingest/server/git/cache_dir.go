package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// cacheDirName is the plugin's directory under the user cache dir.
const cacheDirName = "github-rag-ingest"

// ResolveCacheDir returns the directory for cloned repositories. An empty
// configured value means <user cache dir>/github-rag-ingest, or a fresh
// private temp dir (random name, mode 0700) when that cannot be created. Clones run git checkouts of untrusted
// repositories into this directory, so a configured value must be absolute,
// must not be the filesystem root, and must not be (or sit inside) a
// world-writable directory without the sticky bit, where other local users
// could swap repositories underneath the plugin.
func ResolveCacheDir(configured string) (string, error) {
	if configured == "" {
		if base, err := os.UserCacheDir(); err == nil {
			dir := filepath.Join(base, cacheDirName)
			// A read-only or missing home (some containers) falls through to a temp dir
			if os.MkdirAll(dir, 0o700) == nil {
				return dir, nil
			}
		}
		dir, err := os.MkdirTemp("", cacheDirName+"-")
		if err != nil {
			return "", fmt.Errorf("no cache_path configured and no user cache dir: %w", err)
		}
		return dir, nil
	}

	if !filepath.IsAbs(configured) {
		return "", fmt.Errorf("cache_path %q must be an absolute path", configured)
	}
	dir := filepath.Clean(configured)
	if dir == filepath.Dir(dir) {
		return "", fmt.Errorf("cache_path %q must not be the filesystem root", configured)
	}

	// Check the directory and every existing ancestor up to the root.
	for p := dir; ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Created later with 0700
		case err != nil:
			return "", fmt.Errorf("cache_path %q: %w", configured, err)
		case !info.IsDir():
			return "", fmt.Errorf("cache_path %q: %s is not a directory", configured, p)
		case p == dir && info.Mode().Perm()&0o002 != 0:
			// The cache dir itself must not be world-writable, sticky or not.
			return "", fmt.Errorf("cache_path %q is world-writable", configured)
		case p == dir && ownedByAnotherUser(info):
			// e.g. pre-created in /tmp by another local user
			return "", fmt.Errorf("cache_path %q is owned by another user", configured)
		case info.Mode().Perm()&0o002 != 0 && info.Mode()&fs.ModeSticky == 0:
			return "", fmt.Errorf("cache_path %q is inside %s, which is world-writable without the sticky bit", configured, p)
		}
		if p == filepath.Dir(p) {
			return dir, nil
		}
	}
}
