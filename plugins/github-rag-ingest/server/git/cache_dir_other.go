//go:build !unix

package git

import "io/fs"

// ownedByAnotherUser has no portable equivalent outside Unix.
var ownedByAnotherUser = func(fs.FileInfo) bool { return false }
