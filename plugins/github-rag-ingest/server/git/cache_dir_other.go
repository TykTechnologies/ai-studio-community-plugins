//go:build !unix

package git

import "io/fs"

// ownedByAnotherUser has no portable equivalent outside Unix.
var ownedByAnotherUser = func(fs.FileInfo) bool { return false }

// ancestorUntrusted has no portable equivalent outside Unix.
var ancestorUntrusted = func(fs.FileInfo) bool { return false }
