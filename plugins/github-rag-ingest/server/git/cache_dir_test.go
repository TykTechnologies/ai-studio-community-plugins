package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCacheDir_DefaultIsPrivate(t *testing.T) {
	dir, err := ResolveCacheDir("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, "/tmp/github-rag-cache") {
		t.Errorf("default cache dir %q is the old predictable /tmp path", dir)
	}
	if base, err := os.UserCacheDir(); err == nil && dir != filepath.Join(base, cacheDirName) {
		t.Errorf("default cache dir = %q, want under the user cache dir %q", dir, base)
	}
}

func TestResolveCacheDir_RefusesDangerousValues(t *testing.T) {
	tmp := t.TempDir()

	worldWritable := filepath.Join(tmp, "open")
	if err := os.Mkdir(worldWritable, 0o777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(worldWritable, 0o777) // umask-proof

	sticky := filepath.Join(tmp, "sticky")
	if err := os.Mkdir(sticky, 0o777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(sticky, 0o777|os.ModeSticky)

	for name, value := range map[string]string{
		"root":                          "/",
		"root with dots":                "/tmp/..",
		"relative":                      "cache/repos",
		"world-writable dir":            worldWritable,
		"inside world-writable parent":  filepath.Join(worldWritable, "repos"),
		"world-writable even if sticky": sticky,
	} {
		if dir, err := ResolveCacheDir(value); err == nil {
			t.Errorf("%s: %q accepted as %q", name, value, dir)
		}
	}

	// Inside a sticky world-writable dir (like /tmp) is allowed for a new
	// subdirectory, and a private existing directory is fine.
	private := filepath.Join(tmp, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{filepath.Join(sticky, "repos"), private, filepath.Join(private, "new") + "/"} {
		if _, err := ResolveCacheDir(value); err != nil {
			t.Errorf("%q refused: %v", value, err)
		}
	}
}

func TestResolveCacheDir_RefusesDirOwnedByAnotherUser(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	orig := ownedByAnotherUser
	t.Cleanup(func() { ownedByAnotherUser = orig })
	ownedByAnotherUser = func(info os.FileInfo) bool { return info.Name() == "cache" }

	if _, err := ResolveCacheDir(dir); err == nil || !strings.Contains(err.Error(), "owned by another user") {
		t.Errorf("dir owned by another user: err = %v", err)
	}
}

func TestNewClientCreatesPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repos")
	if _, err := NewClient(dir, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("cache dir mode = %o, want 700", perm)
	}
}
