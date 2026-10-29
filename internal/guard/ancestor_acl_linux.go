package guard

import "golang.org/x/sys/unix"

func ancestorHasNoACL(path string) bool {
	// Missing is the only affirmative ACL-free result. Unreadable/unsupported
	// metadata cannot establish that this directory excludes other users.
	_, err := unix.Getxattr(path, "system.posix_acl_access", nil)
	return err == unix.ENODATA
}
