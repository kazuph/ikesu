package e2e

import "golang.org/x/sys/unix"

// cloneBinary makes a copy-on-write clone: a separate file (its own inode
// and path) that shares the build's blocks on APFS.
func cloneBinary(src, dst string) error {
	return unix.Clonefile(src, dst, 0)
}
