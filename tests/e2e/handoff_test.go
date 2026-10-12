package e2e

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveHandoffKeepsPIDInputAndExit(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var pong api.PongResult
	s.rpc(api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Capabilities["live_handoff"] != true {
		t.Fatalf("handoff capability missing: %+v", pong)
	}
	if help, err := s.cli("server", "--help"); err != nil || !strings.Contains(help, "gtx server live-handoff") {
		t.Fatalf("server help: %v %s", err, help)
	}
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
	beforeServer := s.onlyPTYOwner()
	for _, args := range [][]string{{"--expected-protocol", "999999"}, {"--expected-version", "no-such-version"}, {"--import-exe", "/does/not/exist"}} {
		out, err := s.cli(append([]string{"server", "live-handoff"}, args...)...)
		if err == nil || !strings.Contains(out, "handoff_failed") {
			t.Fatalf("failed handoff %v: %s %v", args, out, err)
		}
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("failed handoff exit must be 1: %v", err)
		}
		message := "handoff stream closed while reading line"
		if args[0] == "--import-exe" {
			message = "failed to spawn handoff import server at /does/not/exist: No such file or directory (os error 2)"
		}
		if !strings.Contains(out, message) {
			t.Fatalf("failure text changed: %s", out)
		}
		t.Logf("failed handoff keeps pane, CLI exit=1: %s", out)
		s.send("printf 'FAILED_HANDOFF_ALIVE\\n'\r")
		s.waitFor("failed handoff retains input", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "FAILED_HANDOFF_ALIVE") })
	}
	if out, err := s.cli("server", "live-handoff", "--expected-protocol", strconv.Itoa(pong.Protocol), "--expected-version", pong.Version); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
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
	if afterServer := s.onlyPTYOwner(); afterServer == beforeServer {
		t.Fatal("server PID did not change")
	}
	s.send("exit\r")
	s.waitFor("imported process exit closes last pane", 15*time.Second, func(tx string) bool { _, err := s.cli("server", "ping"); return err != nil })
}

func (s *session) onlyPTYOwner() string {
	s.t.Helper()
	output, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		s.t.Fatal(err)
	}
	var owners []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, binPath+" --session "+s.name+" server") {
			owners = append(owners, strings.Fields(line)[0])
		}
	}
	if len(owners) != 1 {
		s.t.Fatalf("isolated session server owners: %v", owners)
	}
	fds, err := exec.Command("lsof", "-a", "-p", owners[0], "-Fn").Output()
	if err != nil {
		s.t.Fatalf("server descriptors: %v %s", err, fds)
	}
	count := 0
	for _, line := range strings.Split(string(fds), "\n") {
		if strings.HasPrefix(line, "n/dev/ptmx") || strings.HasPrefix(line, "n/dev/pty") {
			count++
		}
	}
	if count != 1 {
		s.t.Fatalf("one pane must have one parent PTY, got %d: %s", count, fds)
	}
	s.t.Logf("isolated server PID=%s owns exactly %d parent PTY descriptor", owners[0], count)
	return owners[0]
}
