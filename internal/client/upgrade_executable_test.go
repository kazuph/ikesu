package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeExecutableTrustAndReplacement(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".ikesu-upgrade-trust-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ikesu")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	info, sum, err := executableIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	checked := checkedExecutable{path: path, info: info, sum: sum}
	if err := checked.recheck(); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := checked.recheck(); err == nil {
		t.Fatal("replacement was accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executableIdentity(path); err == nil {
		t.Fatal("non-executable file was accepted")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executableIdentity(path); err == nil {
		t.Fatal("writable parent was accepted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executableIdentity(link); err == nil {
		t.Fatal("broken link was accepted")
	}
}

func TestUpgradeAttemptIsScopedToServerGeneration(t *testing.T) {
	failed := upgradeIdentity{Session: "session", Generation: "server-1", Executable: "/release/ikesu", Version: "new"}
	seen := map[upgradeIdentity]bool{failed: true}
	if shouldCheckUpgrade(failed, "old", seen) {
		t.Fatal("the same failed update was retried")
	}
	next := failed
	next.Generation = "server-2"
	if !shouldCheckUpgrade(next, "old", seen) {
		t.Fatal("a new server generation was suppressed")
	}
	rollback := upgradeIdentity{Session: "session", Generation: "server-3", Executable: "/release/old", Version: "old"}
	if !shouldCheckUpgrade(rollback, "new", seen) {
		t.Fatal("a later rollback was suppressed")
	}
	if shouldCheckUpgrade(rollback, "old", seen) {
		t.Fatal("same-version server triggered an upgrade")
	}
	rollback.Executable = ""
	if shouldCheckUpgrade(rollback, "new", seen) {
		t.Fatal("a baseline server without an executable triggered an upgrade")
	}
}
