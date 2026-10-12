package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveHandoffKeepsPIDInputAndExit(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := os.ReadFile(filepath.Join(s.stateDir, s.name, "server.log"))
			t.Logf("server log: %s", b)
		}
	})
	pidfile := filepath.Join(t.TempDir(), "pids")
	s.send(fmt.Sprintf("printf '%%s\\n' $$ > %q; printf 'HANDOFF_READY\\n'\r", pidfile))
	s.waitFor("prepared", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "HANDOFF_READY") })
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--expected-protocol", "999999"}, {"--expected-version", "no-such-version"}, {"--import-exe", "/does/not/exist"}} {
		out, err := s.cli(append([]string{"server", "live-handoff"}, args...)...)
		if err == nil || !strings.Contains(out, "handoff_failed") {
			t.Fatalf("failed handoff %v: %s %v", args, out, err)
		}
		s.send("printf 'FAILED_HANDOFF_ALIVE\\n'\r")
		s.waitFor("failed handoff retains input", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "FAILED_HANDOFF_ALIVE") })
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	if out, err := s.cli("pane", "send-text", "p1", "printf 'HANDOFF_DISPLAY_READY\\n'\r"); err != nil {
		t.Fatalf("imported PTY input: %v %s", err, out)
	}
	s.waitFor("automatic reconnect displays live pane output", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "HANDOFF_DISPLAY_READY") })
	s.send(fmt.Sprintf("printf '%%s\\n' $$ >> %q; printf 'HANDOFF_CONTINUES\\n'\r", pidfile))
	s.waitFor("attached client continues", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "HANDOFF_CONTINUES") })
	after, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before)+string(before) {
		t.Fatalf("shell PID changed: before=%q after=%q", before, after)
	}
	t.Logf("same shell PID=%s; connected client accepted input after handoff", strings.TrimSpace(string(before)))
	s.send("exit\r")
	s.waitFor("imported process exit closes last pane", 15*time.Second, func(tx string) bool { _, err := s.cli("server", "ping"); return err != nil })
}
