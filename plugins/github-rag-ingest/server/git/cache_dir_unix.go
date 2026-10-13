//go:build unix

package git

import (
	"io/fs"
	"os"
	"syscall"
)

func ownedByAnotherUser(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) != os.Getuid()
}
