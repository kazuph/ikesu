package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func executableAncestorFixture(t *testing.T) (base, private, native string) {
	t.Helper()
	// t.TempDir's private ancestors would hide the exposed negative cases.
	base, err := os.MkdirTemp("/tmp", "guard-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	private = filepath.Join(base, ".local")
	lib := filepath.Join(private, "lib")
	if err := os.MkdirAll(lib, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lib, 0775); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	native = filepath.Join(lib, "ikesu")
	if err := os.WriteFile(native, data, 0700); err != nil {
		t.Fatal(err)
	}
	return base, private, native
}

func TestExecutableAncestorsRespectPrivateParent(t *testing.T) {
	base, private, native := executableAncestorFixture(t)
	lib := filepath.Dir(native)
	t.Run("owner-only-parent", func(t *testing.T) {
		if _, _, err := trustedExecutable(native); err != nil {
			t.Fatal("private parent prevents other users reaching writable lib:", err)
		}
	})
	t.Run("read-without-search-parent", func(t *testing.T) {
		if err := os.Chmod(private, 0744); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(private, 0700)
		if _, _, err := trustedExecutable(native); err != nil {
			t.Fatal("read permission without traversal does not expose descendants:", err)
		}
	})
	t.Run("exposed-group-writable-parent", func(t *testing.T) {
		if err := os.Chmod(private, 0755); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(private, 0700)
		if _, _, err := trustedExecutable(native); err == nil {
			t.Fatal("exposed writable ancestor trusted")
		}
	})
	t.Run("writable-parent-can-replace-private-directory", func(t *testing.T) {
		if err := os.Chmod(base, 0777); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(base, 0755)
		if _, _, err := trustedExecutable(native); err == nil {
			t.Fatal("private child hid replaceable ancestor")
		}
	})
	t.Run("writable-executable-still-rejected", func(t *testing.T) {
		if err := os.Chmod(native, 0775); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(native, 0700)
		if _, _, err := trustedExecutable(native); err == nil {
			t.Fatal("private parent allowed writable executable")
		}
	})
	t.Run("symlink-escapes-private-parent", func(t *testing.T) {
		outside := filepath.Join(base, "shared")
		if err := os.Mkdir(outside, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(outside, 0777); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(outside, "ikesu")
		data, err := os.ReadFile(native)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(external, data, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(lib, "external")
		if err := os.Symlink(external, link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := trustedExecutable(link); err == nil {
			t.Fatal("private lexical prefix hid exposed resolved ancestor")
		}
	})
}

func TestAncestorACLReadFailureIsNotProtection(t *testing.T) {
	if ancestorHasNoACL(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("unreadable ACL metadata was treated as ACL-free")
	}
}
