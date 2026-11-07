//go:build darwin

package guard

import (
	"os"
	"syscall"
)

// snapshotStat drops the access time: running the guard runtime updates the
// executable's atime on every command, which is not a change to its content.
func snapshotStat(fi os.FileInfo) any {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Sys()
	}
	c := *st
	c.Atimespec = syscall.Timespec{}
	return c
}
