package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestNestedGuardReasonRequiredBeforeRealDecision(t *testing.T) {
	for _, tc := range []struct {
		name, own, parent string
		dialog            bool
	}{
		{"missing", "", "", false},
		{"own-english", "Please approve this command", "", false},
		{"parent-english", "", "Please approve the outer command", false},
		{"inherited-japanese", "", "外側のコマンドから申請文を引き継ぐ試験です。実行前に却下します。", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := start(t)
			s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "nested-reason-test", Agent: "claude", State: "idle"}, nil)
			root := s.stateDir
			if err := os.Mkdir(filepath.Join(root, "home"), 0700); err != nil {
				t.Fatal(err)
			}
			victim := root + "/home/../victim"
			if err := os.WriteFile(victim, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "guard", "exec", "--", "/bin/rm", victim)
			cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "AI_GUARD_REQUEST_REASON=" + tc.own, "IKESU_GUARD_PARENT_COMMAND=bash outer-request.sh", "IKESU_GUARD_PARENT_REASON=" + tc.parent}
			cmd.Dir = root
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if tc.dialog {
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				finished := make(chan error, 1)
				go func() { finished <- cmd.Wait() }()
				var pending api.DecisionListResult
				s.waitFor("inherited reason decision", 5*time.Second, func(string) bool {
					s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
					return len(pending.Decisions) == 1
				})
				d := pending.Decisions[0]
				if d.Body == nil || !strings.Contains(*d.Body, "外側のコマンド bash outer-request.sh から引き継ぎ") || !strings.Contains(*d.Body, tc.parent) {
					t.Fatal("valid inherited context lost")
				}
				if out, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
					t.Fatal(out, err)
				}
				if err := <-finished; err == nil {
					t.Fatal("rejected command succeeded")
				}
			} else {
				if err := cmd.Run(); err == nil {
					t.Fatal("invalid nested reason succeeded")
				}
				if !strings.Contains(output.String(), "外側のコマンド: bash outer-request.sh") || !strings.Contains(output.String(), "申請文") {
					t.Fatal("missing reason hint or outer command", output.String())
				}
				for _, status := range []string{"pending", "answered", "expired", "cancelled"} {
					var list api.DecisionListResult
					s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: status}, &list)
					if len(list.Decisions) != 0 {
						t.Fatalf("invalid reason created a %s decision", status)
					}
				}
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != "unchanged" {
				t.Fatal("victim changed", err)
			}
			t.Logf("nested reason %s dialog=%v victim=unchanged", tc.name, tc.dialog)
		})
	}
}
