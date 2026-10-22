package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestDecisionGuardInvisibleCLIApproveAndReject(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "nodialog-test", Agent: "codex", State: "idle"}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	for _, choice := range []string{"approve", "reject"} {
		t.Run(choice, func(t *testing.T) {
			cmd := exec.Command(binPath, "--session", s.name, "guard", "exec", "--", "/bin/echo", "deploy-proof-"+choice)
			cmd.Dir = t.TempDir()
			view := *s
			view.t = t
			cmd.Env = append(os.Environ(), "IKESU_STATE_DIR="+s.stateDir,
				"IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH="+api.APISocketPath(filepath.Join(s.stateDir, s.name)),
				"IKESU_GUARD_MARKER="+marker, "AI_GUARD_REQUEST_REASON=隔離環境で画面表示と承認後の実行を確認する")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
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
			var pending api.DecisionListResult
			view.waitFor("guard pending", 5*time.Second, func(string) bool {
				view.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				return len(pending.Decisions) == 1
			})
			d := pending.Decisions[0]
			if d.Kind != "command_guard" || d.Status != "pending" || d.Origin == nil || d.Origin.PaneID != "p1" {
				t.Fatal(d)
			}
			select {
			case err := <-done:
				finished = true
				t.Fatal("command ran before answer", err)
			default:
			}
			s.send("printf 'PENDING_%s\\n' INPUT\r")
			view.waitFor("input while guard pending", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "PENDING_INPUT") })
			for _, forbidden := range []string{d.Title, "! 1", "esc close", "1日許可"} {
				if strings.Contains(s.text(), forbidden) {
					t.Fatalf("guard UI leaked %q:\n%s", forbidden, s.text())
				}
			}
			if dir := os.Getenv("NODIALOG_EVIDENCE_DIR"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, "guard-"+choice+"-pending.screen.txt"), []byte(s.text()), 0600); err != nil {
					t.Fatal(err)
				}
				data, err := json.MarshalIndent(d, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "guard-"+choice+"-pending.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if raw, err := s.cli("decision", "answer", d.DecisionID, choice); err != nil {
				t.Fatal(raw, err)
			}
			select {
			case err := <-done:
				finished = true
				if choice == "approve" && (err != nil || stdout.String() != "deploy-proof-approve\n") {
					t.Fatal("approved command did not execute", err, stdout.String(), stderr.String())
				}
				if choice == "reject" && (err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Command cancelled")) {
					t.Fatal("rejected command executed", err, stdout.String(), stderr.String())
				}
				t.Logf("choice=%s exit=%d stdout=%q stderr=%q", choice, cmd.ProcessState.ExitCode(), stdout.String(), stderr.String())
			case <-time.After(5 * time.Second):
				t.Fatal("guard did not finish after CLI answer")
			}
		})
	}
}
