package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestGuardHandoffThroughPrivateInstallTree(t *testing.T) {
	t.Parallel()
	old := os.Getenv("GUARD_ENTRY_BASELINE_BINARY")
	if old == "" {
		old = binPath
	}
	s := startVersionMode(t, nil, false, old, "")
	s.waitFor("old shell prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "private-install-handoff", Agent: "codex", State: "idle"}, nil)
	pidfile := filepath.Join(s.stateDir, "shell-pid")
	s.send("printf '%s' $$ > '" + pidfile + "'\r")
	s.waitFor("old shell pid", 5*time.Second, func(string) bool { _, err := os.Stat(pidfile); return err == nil })
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(t.TempDir(), ".local", "lib")
	if err := os.MkdirAll(lib, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lib, 0775); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(lib, "ikesu")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(next, data, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", next); err != nil {
		t.Fatalf("handoff through owner-only .local and lib 0775: %v %s", err, out)
	}
	entry, err := os.Stat(filepath.Join(s.stateDir, "guard", "shell", "p_1", "ikesu"))
	expected, statErr := os.Stat(next)
	if err != nil || statErr != nil || !os.SameFile(entry, expected) {
		t.Fatalf("guard entry did not bind new executable: %v %v", err, statErr)
	}
	if err := os.Remove(pidfile); err != nil {
		t.Fatal(err)
	}
	s.send("printf '%s' $$ > '" + pidfile + "'; printf 'PRIVATE_%s\\n' HANDOFF\r")
	s.waitFor("surviving shell output", 5*time.Second, func(text string) bool { return strings.Contains(text, "PRIVATE_HANDOFF") })
	after, err := os.ReadFile(pidfile)
	if err != nil || string(before) != string(after) {
		t.Fatalf("shell pid changed: %s -> %s: %v", before, after, err)
	}
	t.Logf("baseline=%s next=%s shell=%s", old, next, before)
}
