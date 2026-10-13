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

// ancestorUntrusted reports an ancestor directory owned by neither root nor
// the plugin's user. A variable so tests can simulate a foreign owner.
var ancestorUntrusted = func(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid != 0 && int(st.Uid) != os.Getuid()
}
