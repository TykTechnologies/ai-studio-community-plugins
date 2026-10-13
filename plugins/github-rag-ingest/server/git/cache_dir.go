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
// could swap repositories underneath the plugin. Every existing ancestor
// must be owned by root or the plugin's user (its owner could rename or
// replace anything below it), and the directory itself must not be a
// symlink. NewClient checks the directory again after creating it.
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

	// The directory itself, if it exists. Lstat: a symlink is refused.
	switch info, err := os.Lstat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		// Created later with 0700, then checked again by NewClient
	case err != nil:
		return "", fmt.Errorf("cache_path %q: %w", configured, err)
	default:
		if err := checkCacheDir(configured, info); err != nil {
			return "", err
		}
	}

	// Every existing ancestor up to the root. A symlinked ancestor must itself
	// be owned by root or the plugin's user (its owner could re-point it), and
	// is then checked at its target, so system links such as
	// /var -> /private/var pass.
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		if link, err := os.Lstat(p); err == nil && link.Mode()&fs.ModeSymlink != 0 && ancestorUntrusted(link) {
			return "", fmt.Errorf("cache_path %q is inside %s, a symlink owned by another user", configured, p)
		}
		info, err := os.Stat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Created later with 0700
		case err != nil:
			return "", fmt.Errorf("cache_path %q: %w", configured, err)
		case !info.IsDir():
			return "", fmt.Errorf("cache_path %q: %s is not a directory", configured, p)
		case info.Mode().Perm()&0o002 != 0 && info.Mode()&fs.ModeSticky == 0:
			return "", fmt.Errorf("cache_path %q is inside %s, which is world-writable without the sticky bit", configured, p)
		case ancestorUntrusted(info):
			return "", fmt.Errorf("cache_path %q is inside %s, which is owned by another user", configured, p)
		}
		if p == filepath.Dir(p) {
			return dir, nil
		}
	}
}

// checkCacheDir checks the cache directory itself, from an Lstat result.
func checkCacheDir(configured string, info fs.FileInfo) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("cache_path %q is a symlink", configured)
	case !info.IsDir():
		return fmt.Errorf("cache_path %q is not a directory", configured)
	case info.Mode().Perm()&0o002 != 0:
		// The cache dir itself must not be world-writable, sticky or not.
		return fmt.Errorf("cache_path %q is world-writable", configured)
	case ownedByAnotherUser(info):
		// e.g. pre-created in /tmp by another local user
		return fmt.Errorf("cache_path %q is owned by another user", configured)
	}
	return nil
}

// ensureCacheDir creates dir (0700) and checks it again, so a directory
// another user created or replaced since ResolveCacheDir is refused.
func ensureCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("cache directory: %w", err)
	}
	return checkCacheDir(dir, info)
}
