package client

import (
	"net"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
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

// Two isolated real servers provide actual ping identity and a socket reconnect.
func TestUpgradeFailureIsNotRetriedAfterReconnect(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	h := newRightMachineHarness(t)
	m := h.m
	teststate.Setenv(t, "IKESU_STATE_DIR", filepath.Dir(h.dirs["local"]))
	m.failedUpgrades = make(map[upgradeIdentity]bool)
	m.clientVersion = "older-client"
	e := m.endpoints["local"]
	receiveUpgrade := func() endpointEvent {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case event := <-m.events:
				if event.upgrade != nil {
					return event
				}
				m.event(event)
			case <-timer.C:
				t.Fatal("upgrade check did not return")
				return endpointEvent{}
			}
		}
	}
	m.checkUpgrade(e.attempt)
	failure := receiveUpgrade()
	if failure.upgrade.err == nil || failure.upgrade.identity.Generation == "" {
		t.Fatalf("first check did not fail with server identity: %+v", failure.upgrade)
	}
	m.event(failure)
	if _, err := os.Stat(filepath.Join(h.dirs["local"], "client-upgrade.log")); err != nil {
		t.Fatalf("upgrade failure was not persisted: %v", err)
	}
	if !m.failedUpgrades[failure.upgrade.identity] {
		t.Fatal("real check failure was not recorded")
	}
	e.connection.Close()
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("local", stream)
	attempt := e.attempt
	h.until("reconnected after failed upgrade", func() bool { return e.connection != nil && e.snapshotAttempt == attempt })
	m.checkUpgrade(attempt)
	repeated := receiveUpgrade()
	if repeated.upgrade.err != nil || repeated.upgrade.candidate != nil || repeated.upgrade.identity != failure.upgrade.identity {
		t.Fatalf("same failed upgrade was checked or executed again: %+v", repeated.upgrade)
	}
}
