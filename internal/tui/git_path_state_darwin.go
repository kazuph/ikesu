//go:build darwin

package tui

import (
	"os"
	"syscall"
)

func sameGitFileState(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	// ctime retains writes even when a caller restores mtime and file size.
	return xok && yok && x.Ctimespec == y.Ctimespec && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
