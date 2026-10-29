package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestGuardEntryUpgradeKeepsClaudePane(t *testing.T) {
	t.Parallel()
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	// The optional baseline is built from origin/main on mini. Without it this
	// also proves the normal same-version handoff path in the complete suite.
	old := os.Getenv("GUARD_ENTRY_BASELINE_BINARY")
	if old == "" {
		old = binPath
	}
	s := startVersionMode(t, nil, false, old, "")
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	dir := filepath.Join(s.stateDir, "guard", "shell", "p_1")
	prefix := filepath.Join(dir, "claude-prefix")
	// A real long-lived pane process launches the inherited prefix for each
	// command, as Claude does. No agent implementation or authorization is mocked.
	result := filepath.Join(s.stateDir, "result")
	launcher := "printf 'READY_%s\\n' GUARD; while read gate; do " + quote(prefix) + " " + quote("/bin/bash -c 'printf next' > "+quote(result)) + "; printf 'FINISHED_%s\\n' \"$gate\"; done"
	s.send("/bin/sh -c " + quote(launcher) + "\r")
	s.waitFor("persistent launcher", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_GUARD") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "entry-upgrade-test", Agent: "claude", State: "idle"}, nil)
	// Native inode evidence distinguishes current executable from the old one
	// even when the production version string is deliberately not bumped.
	next := filepath.Join(t.TempDir(), "ikesu-next")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(next, data, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", next); err != nil {
		t.Fatal(out, err)
	}
	current, err := os.Stat(filepath.Join(dir, "ikesu"))
	expected, _ := os.Stat(next)
	if err != nil || !os.SameFile(current, expected) {
		t.Fatal("entry did not switch", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "bash-env.current"))
	if err != nil || !strings.Contains(string(body), next) {
		t.Fatal("generator did not switch", err)
	}
	if strings.Contains(string(body), "'"+old+"' guard") {
		t.Fatal("payload retains old executable")
	}
	s.send("go\r")
	s.waitFor("next Claude command", 5*time.Second, func(string) bool { b, e := os.ReadFile(result); return e == nil && string(b) == "next" })
	// Missing/corrupt fixed entry must stop before the requested command writes.
	for _, corrupt := range []bool{false, true} {
		if err := os.Remove(filepath.Join(dir, "ikesu")); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			if err := os.WriteFile(filepath.Join(dir, "ikesu"), []byte("invalid executable\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(result); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		gate := "missing"
		if corrupt {
			gate = "corrupt"
		}
		s.send(gate + "\r")
		s.waitFor("entry failed closed", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "FINISHED_"+gate) })
		if _, err := os.Stat(result); !os.IsNotExist(err) {
			t.Fatal("command ran with unavailable runtime", err)
		}
		if err := os.Remove(filepath.Join(dir, "ikesu")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Symlink(next, filepath.Join(dir, "ikesu")); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("persistent Claude-like pane used native %s; source=%s; missing/corrupt entry stopped execution", next, old)
}
