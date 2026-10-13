//go:build unix

package git

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByAnotherUser is a variable so tests can simulate a foreign owner.
var ownedByAnotherUser = func(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) != os.Getuid()
}
