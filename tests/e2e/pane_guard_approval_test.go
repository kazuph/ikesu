package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestPaneGuardApprovalExecutesRealCommand(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	startup := filepath.Join(s.stateDir, "guard", "shell", "p_1", "bash-env")
	result := filepath.Join(s.stateDir, "approved-result")
	reason := "対象プロジェクト: ikesu隔離試験。必要な理由: ペインの承認を検証。実施内容: 無害なコマンドを実行。"
	command := fmt.Sprintf("printf 'READY_GUARD\\n'; read gate; /bin/dd if=/dev/null of=/dev/null && printf success > %s; printf 'GUARD_FINISHED\\n'", shQuote(result))
	s.send(fmt.Sprintf("AI_GUARD_REQUEST_REASON=%s BASH_ENV=%s /bin/bash -c %s\r", shQuote(reason), shQuote(startup), shQuote(command)))
	s.waitFor("ready", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_GUARD") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "approval-test", Agent: "claude", State: "idle"}, nil)
	s.send("go\r")
	var pending api.DecisionListResult
	s.waitFor("pending", 5*time.Second, func(string) bool {
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	if d := pending.Decisions[0]; d.Kind != "command_guard" || d.Origin == nil || d.Origin.PaneID != "p1" || d.Origin.OutsidePane {
		t.Fatalf("decision origin: %+v", d)
	}
	t.Logf("first guard command: %q", pending.Decisions[0].Origin.Command)
	s.waitFor("answer indicator", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "Answer (1)") })
	save := func(name string) {
		t.Helper()
		if dir := os.Getenv("PANE_APPROVAL_EVIDENCE"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(s.text()), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	save("indicator")
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatal("command ran before approval")
	}
	s.send("\x02a")
	s.waitFor("approval visible", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "申請文") && strings.Contains(tx, "1: 承認") })
	save("open")
	s.send("\x1b")
	s.waitFor("closed without answer", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "1: 承認") && strings.Contains(tx, "Answer (1)") })
	save("escape")
	s.send("\x02a")
	s.waitFor("reopened", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "1: 承認") })
	s.send("1")
	s.waitFor("selection", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "● 1: 承認") })
	save("selected")
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatal("number answered without Enter")
	}
	s.send("\r")
	s.waitFor("answered or error", 5*time.Second, func(tx string) bool {
		var all api.DecisionListResult
		s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &all)
		for _, d := range all.Decisions {
			if d.DecisionID == pending.Decisions[0].DecisionID && d.Status != "pending" {
				if d.Status != "answered" || d.Answer == nil || d.Answer.OptionID == nil || *d.Answer.OptionID != "approve" {
					t.Fatalf("unexpected decision result: %+v", d)
				}
				return true
			}
		}
		if strings.Contains(tx, "decision_") || strings.Contains(tx, "endpoint_action_") {
			t.Fatalf("answer error: %s", tx)
		}
		return false
	})
	s.waitFor("command finished", 5*time.Second, func(string) bool {
		var remaining api.DecisionListResult
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &remaining)
		if len(remaining.Decisions) > 0 {
			t.Fatalf("new guard decision after approval: %+v", remaining.Decisions[0])
		}
		b, err := os.ReadFile(result)
		return err == nil && string(b) == "success"
	})
	b, err := os.ReadFile(result)
	if err != nil || string(b) != "success" {
		t.Fatalf("guard command result %q: %v", b, err)
	}
	save("confirmed")
}
