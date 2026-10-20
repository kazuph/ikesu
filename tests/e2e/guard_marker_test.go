package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
)

func TestGuardMarkerRoutingAfterRealHandoff(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if e := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[guard]\nask_timeout_seconds=1\n"), 0600); e != nil {
		t.Fatal(e)
	}
	s := startEnv(t, map[string]string{"GTX_CONFIG": cfg})
	s.waitFor("shell", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "guard-marker-test", Agent: "codex", State: "idle"}, nil)
	// A different executable name changes StateDir. The inherited listener,
	// not a guessed pathname under that new namespace, must remain authoritative.
	next := filepath.Join(t.TempDir(), "gtx-guard-next")
	data, e := os.ReadFile(binPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(next, data, 0700); e != nil {
		t.Fatal(e)
	}
	if out, e := s.cli("server", "live-handoff", "--import-exe", next); e != nil {
		t.Fatal(e, out)
	}
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	data, e = os.ReadFile(marker)
	if e != nil {
		t.Fatal(e)
	}
	var m guard.Marker
	if e = json.Unmarshal(data, &m); e != nil {
		t.Fatal(e)
	}
	socket := api.APISocketPath(filepath.Join(s.stateDir, s.name))
	if m.Socket != socket {
		t.Fatalf("marker points at %q, live listener %q", m.Socket, socket)
	}
	t.Logf("handoff marker=%s socket=%s next=%s", marker, m.Socket, next)
	// The already-running shell keeps its PID and stale environment. Its fixed
	// generated entry must still talk to the inherited server endpoint.
	// Run through the pre-handoff shell itself, rather than only invoking a
	// generated shim from a new host process.
	for _, tc := range []struct {
		choice   string
		cleanEnv bool
	}{{"approve", false}, {"reject", false}, {"approve", true}, {"reject", true}} {
		choice := tc.choice
		command := "rm"
		if tc.cleanEnv {
			command = "/usr/bin/env -i /bin/rm"
		}
		dir := t.TempDir()
		home := filepath.Join(dir, "home")
		if e = os.Mkdir(home, 0700); e != nil {
			t.Fatal(e)
		}
		target := home + "/../old-pane-victim"
		if e = os.WriteFile(target, []byte("test-only"), 0600); e != nil {
			t.Fatal(e)
		}
		status := filepath.Join(dir, "status")
		s.send(fmt.Sprintf("AI_GUARD_REQUEST_REASON=古い画面の承認動作を確認する AI_GUARD_TEMP_APPROVAL_FILE=%q GTX_GUARD_MARKER= GTX_SOCKET_PATH=/missing.sock GTX_PANE_ID=p999 %s %q; printf '%%s' $? > %q\r", filepath.Join(dir, "approval"), command, target, status))
		var pending api.DecisionListResult
		s.waitFor("old shell pending", 5*time.Second, func(string) bool {
			s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
			return len(pending.Decisions) == 1
		})
		if _, e = os.Stat(target); e != nil {
			t.Fatal("old shell executed before approval", e)
		}
		if out, e := s.cli("decision", "answer", pending.Decisions[0].DecisionID, choice); e != nil {
			t.Fatal(e, out)
		}
		if choice == "reject" {
			s.waitFor("old shell rejected before accept-line", 5*time.Second, func(string) bool {
				// The pane can wrap inside "Command cancelled"; inspect the
				// same PTY output with its soft wraps joined, without UI borders.
				var output api.PaneReadResultEnvelope
				s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", Source: "recent-unwrapped", StripANSI: true}, &output)
				return strings.Contains(output.Read.Text, "Command cancelled")
			})
			if _, e = os.Stat(target); e != nil {
				t.Fatal("rejected old-shell command executed", e)
			}
			if _, e = os.Stat(status); !os.IsNotExist(e) {
				t.Fatal("trailing command executed after line rejection", e)
			}
			s.send("\x15")
			t.Log("old shell rejected the whole line; target retained; trailing printf did not run")
			continue
		}
		s.waitFor("old shell resolved", 5*time.Second, func(string) bool { _, e := os.Stat(status); return e == nil })
		b, e := os.ReadFile(status)
		if e != nil {
			t.Fatal(e)
		}
		_, fileErr := os.Stat(target)
		if choice == "approve" && (string(b) != "0" || !os.IsNotExist(fileErr)) {
			t.Fatal(choice, string(b), fileErr)
		}
		if choice == "reject" && (string(b) == "0" || fileErr != nil) {
			t.Fatal(choice, string(b), fileErr)
		}
		t.Logf("old shell choice=%s status=%s removed=%t", choice, b, os.IsNotExist(fileErr))
	}
	for _, choice := range []string{"approve", "reject", "timeout", "day_allow"} {
		t.Run(choice, func(t *testing.T) {
			dir := t.TempDir()
			home := filepath.Join(dir, "home")
			if e := os.Mkdir(home, 0700); e != nil {
				t.Fatal(e)
			}
			target := home + "/../victim"
			if e := os.WriteFile(target, []byte("test-only"), 0600); e != nil {
				t.Fatal(e)
			}
			shim := filepath.Join(s.stateDir, "guard", "bin", "p_1", "rm")
			cmd := exec.Command("/bin/zsh", "-c", `GTX_GUARD_MARKER= GTX_PANE_ID=p999 GTX_SOCKET_PATH=/missing.sock "$1" "$2"`, "guard-test", shim, target)
			approval := filepath.Join(dir, "approval")
			cmd.Env = append(os.Environ(), "GTX_CONFIG="+cfg, "GTX_STATE_DIR="+s.stateDir, "AI_GUARD_TEMP_APPROVAL_FILE="+approval, "AI_GUARD_REQUEST_REASON=隔離した検査用ファイルの削除を確認する")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			finished := false
			defer func() {
				if !finished {
					_ = cmd.Process.Kill()
					<-done
				}
			}()
			view := *s
			view.t = t
			var pending api.DecisionListResult
			view.waitFor("guard pending", 5*time.Second, func(string) bool {
				view.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				return len(pending.Decisions) == 1
			})
			d := pending.Decisions[0]
			if d.Origin == nil || d.Origin.PaneID != "p1" || d.Kind != "command_guard" {
				t.Fatal(d)
			}
			if _, e := os.Stat(target); e != nil {
				t.Fatal("executed before answer", e)
			}
			if choice != "timeout" {
				if out, e := s.cli("decision", "answer", d.DecisionID, choice); e != nil {
					t.Fatal(e, out)
				}
			}
			select {
			case e := <-done:
				finished = true
				_, fileErr := os.Stat(target)
				if choice == "approve" || choice == "day_allow" {
					if e != nil || !os.IsNotExist(fileErr) {
						t.Fatal(e, fileErr, stderr.String())
					}
				} else if e == nil || fileErr != nil || !strings.Contains(stderr.String(), "Command cancelled") {
					t.Fatal(e, fileErr, stderr.String())
				}
				t.Logf("choice=%s exit=%d removed=%t stderr=%q", choice, cmd.ProcessState.ExitCode(), os.IsNotExist(fileErr), stderr.String())
			case <-time.After(5 * time.Second):
				t.Fatal("decision did not resolve")
			}
			if choice == "day_allow" {
				// A distinct ask command in the same cwd is covered by the persisted day grant.
				if e := os.WriteFile(target, []byte("test-only"), 0600); e != nil {
					t.Fatal(e)
				}
				follow := exec.Command(shim, target)
				follow.Env = cmd.Env
				follow.Dir = cmd.Dir
				if b, e := follow.CombinedOutput(); e != nil {
					t.Fatal("day allowance lost", e, string(b))
				}
				if _, e := os.Stat(target); !os.IsNotExist(e) {
					t.Fatal("day-allowed deletion not executed", e)
				}
			}
		})
	}
}
