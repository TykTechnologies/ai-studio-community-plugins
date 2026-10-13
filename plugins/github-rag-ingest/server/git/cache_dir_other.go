//go:build !unix

package git

import "io/fs"

// ownedByAnotherUser has no portable equivalent outside Unix.
func ownedByAnotherUser(fs.FileInfo) bool { return false }
