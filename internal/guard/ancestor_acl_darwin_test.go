package guard

import (
	"os"
	"os/exec"
	"testing"
)

func TestExecutableAncestorWithSearchACLRejected(t *testing.T) {
	_, private, native := executableAncestorFixture(t)
	if !ancestorHasNoACL(private) {
		t.Fatal("new owner-only directory has no readable ACL-free state")
	}
	// Grant a different user traversal while the POSIX mode stays 0700.
	if out, err := exec.Command("/bin/chmod", "+a", "user:nobody allow search", private).CombinedOutput(); err != nil {
		t.Fatalf("set search ACL: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/bin/chmod", "-N", private).Run() })
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("ACL fixture mode changed: %v %v", info, err)
	}
	if ancestorHasNoACL(private) {
		t.Fatal("native ACL query missed search entry")
	}
	if _, _, err := trustedExecutable(native); err == nil {
		t.Fatal("ACL-enabled 0700 parent hid writable lib")
	}
}

func TestExecutableAncestorWithMutableUpperACLRejected(t *testing.T) {
	base, private, native := executableAncestorFixture(t)
	if !ancestorHasNoACL(private) {
		t.Fatal("protected child must remain ACL-free")
	}
	if out, err := exec.Command("/bin/chmod", "+a", "user:nobody allow search,list,add_file,add_subdirectory,delete_child", base).CombinedOutput(); err != nil {
		t.Fatalf("set upper ACL: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/bin/chmod", "-N", base).Run() })
	info, err := os.Stat(base)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("upper ACL fixture mode changed: %v %v", info, err)
	}
	if ancestorHasNoACL(base) || !ancestorHasNoACL(private) {
		t.Fatal("non-inherited ACL was not confined to upper ancestor")
	}
	if _, _, err := trustedExecutable(native); err == nil {
		t.Fatal("upper ACL can replace ACL-free private parent")
	}
}
