package guard

import (
	"encoding/binary"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExecutableAncestorWithAccessACLRejected(t *testing.T) {
	_, private, native := executableAncestorFixture(t)
	if !ancestorHasNoACL(private) {
		t.Fatal("new owner-only directory has no readable ACL-free state")
	}
	// Linux UAPI posix_acl_xattr.h: version 2, little-endian (tag, perm, uid).
	// A zero mask keeps mode 0700, while retaining an extended named-user entry.
	acl := make([]byte, 4)
	binary.LittleEndian.PutUint32(acl, 2)
	for _, e := range []struct {
		tag, perm uint16
		id        uint32
	}{{1, 7, ^uint32(0)}, {2, 1, uint32(os.Geteuid() + 1)}, {4, 0, ^uint32(0)}, {16, 0, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		var entry [8]byte
		binary.LittleEndian.PutUint16(entry[:2], e.tag)
		binary.LittleEndian.PutUint16(entry[2:4], e.perm)
		binary.LittleEndian.PutUint32(entry[4:], e.id)
		acl = append(acl, entry[:]...)
	}
	if err := unix.Setxattr(private, "system.posix_acl_access", acl, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("ACL fixture mode changed: %v %v", info, err)
	}
	if ancestorHasNoACL(private) {
		t.Fatal("native ACL query missed access entries")
	}
	if _, _, err := trustedExecutable(native); err == nil {
		t.Fatal("ACL-enabled 0700 parent hid writable lib")
	}
}
