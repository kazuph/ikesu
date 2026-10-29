package guard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeEntryGenerationAndProtection(t *testing.T) {
	i, a := scriptPane(t)
	path := a.RuntimePath()
	exe, _ := os.Executable()
	actual, err := os.Stat(path)
	own, _ := os.Stat(exe)
	if err != nil || !os.SameFile(actual, own) {
		t.Fatal("entry is not native executable", err)
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil || directory.Mode().Perm() != 0700 {
		t.Fatal("entry directory permissions", directory, err)
	}
	for _, line := range []string{
		"printf replacement > " + quote(path), "tee " + quote(path), "chmod u+w " + quote(path),
		"ln -sf /bin/true " + quote(path), "tee " + quote(exe), "chmod u+w " + quote(exe), "mv /tmp/replacement " + quote(filepath.Dir(path)),
	} {
		_, v, _ := lineVerdict(Config{}, i, line)
		if v.Decision == "allow" {
			t.Fatalf("guard entry modification allowed: %s: %+v", line, v)
		}
	}
	for _, name := range []string{"bash-env", "bash-env.current", ".zshenv", ".zshenv.current", "claude-prefix"} {
		name := filepath.Join(filepath.Dir(path), name)
		before, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := RefreshStartup(i); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(name)
		if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("same generator rewrote file", name, err)
		}
	}
	// Existing readers retain the complete old inode while a newer generator
	// replaces the path. The native entry itself does not depend on caller env.
	payload := filepath.Join(filepath.Dir(path), "bash-env.current")
	old := []byte("# old generator\n")
	if err := os.WriteFile(payload, old, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := RefreshStartup(i); err != nil {
		t.Fatal(err)
	}
	held := make([]byte, len(old))
	if _, err := reader.ReadAt(held, 0); err != nil || !bytes.Equal(held, old) {
		t.Fatal("old reader changed", string(held), err)
	}
	data, _ := os.ReadFile(payload)
	if !strings.Contains(string(data), a.RuntimePath()) || bytes.Equal(data, old) {
		t.Fatal("new generator not installed")
	}
	delete(i.Env, "IKESU_GUARD_EXECUTABLE")
	if _, ok := paneBashEnv(i); !ok {
		t.Fatal("inherited version pin still required")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := RefreshStartup(i); err == nil {
		t.Fatal("missing native runtime allowed")
	}
}
