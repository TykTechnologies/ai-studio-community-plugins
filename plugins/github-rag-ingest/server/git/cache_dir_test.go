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

// A symlink as the final component is refused: it could point anywhere,
// including into another user's directory.
func TestResolveCacheDir_RefusesSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ResolveCacheDir(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlinked cache_path: err = %v", err)
	}
}

// Every existing ancestor must be owned by root or the plugin's user: the
// owner of a directory can rename or replace anything below it.
func TestResolveCacheDir_RefusesAncestorOwnedByAnotherUser(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := ancestorUntrusted
	t.Cleanup(func() { ancestorUntrusted = orig })
	ancestorUntrusted = func(info os.FileInfo) bool { return info.Name() == "shared" }

	if _, err := ResolveCacheDir(filepath.Join(parent, "repos")); err == nil || !strings.Contains(err.Error(), "owned by another user") {
		t.Errorf("ancestor owned by another user: err = %v", err)
	}
}

// NewClient re-checks the directory after creating it, so one another user
// pre-created between ResolveCacheDir and MkdirAll (e.g. under /tmp), or
// swapped for a symlink, is refused rather than used.
func TestNewClientRechecksAfterCreate(t *testing.T) {
	tmp := t.TempDir()

	target := filepath.Join(tmp, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	swapped := filepath.Join(tmp, "swapped")
	if err := os.Symlink(target, swapped); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewClient(swapped, nil); err == nil {
		t.Error("NewClient accepted a symlinked cache dir")
	}

	foreign := filepath.Join(tmp, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	orig := ownedByAnotherUser
	t.Cleanup(func() { ownedByAnotherUser = orig })
	ownedByAnotherUser = func(info os.FileInfo) bool { return info.Name() == "foreign" }
	if _, err := NewClient(foreign, nil); err == nil || !strings.Contains(err.Error(), "owned by another user") {
		t.Errorf("NewClient with a foreign-owned dir: err = %v", err)
	}

	open := filepath.Join(tmp, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(open, 0o777)
	if _, err := NewClient(open, nil); err == nil {
		t.Error("NewClient accepted a world-writable cache dir")
	}
}
