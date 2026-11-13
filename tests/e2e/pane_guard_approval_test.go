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

func TestReviewGuardRejectReasonWithDigits(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	s := start(t)
	s.waitFor("shell", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	result := filepath.Join(s.stateDir, "rejected-result")
	reason := "対象プロジェクト: ikesu隔離試験。必要な理由: 却下の入力を検証。実施内容: 無害なコマンドを拒否。"
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "approval-test", Agent: "claude", State: "idle"}, nil)
	s.send(fmt.Sprintf("AI_GUARD_REQUEST_REASON=%s /usr/bin/curl -X POST https://example.invalid/x && printf success > %s\r", shQuote(reason), shQuote(result)))
	var pending api.DecisionListResult
	s.waitFor("pending", 5*time.Second, func(string) bool {
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	if d := pending.Decisions[0]; d.Kind != "command_guard" || d.Origin == nil || d.Origin.PaneID != "p1" || d.Origin.OutsidePane || !strings.Contains(d.Origin.Command, "/usr/bin/curl") {
		t.Fatalf("decision origin: %+v", d)
	}
	id := pending.Decisions[0].DecisionID
	s.send("\x02a")
	s.waitFor("approval", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "1  承認") })
	s.send("312")
	s.waitFor("reject reason", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "reject note 12") && strings.Contains(tx, "3  却下")
	})
	if dir := os.Getenv("PANE_REVIEW_EVIDENCE"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "reject-reason.txt"), []byte(s.text()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s.send("\r")
	s.waitFor("rejected", 5*time.Second, func(string) bool {
		var got api.DecisionResult
		s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: id}, &got)
		return got.Decision.Answer != nil
	})
	var got api.DecisionResult
	s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: id}, &got)
	answer := got.Decision.Answer
	if answer == nil || answer.OptionID == nil || *answer.OptionID != "reject" || answer.Text == nil || *answer.Text != "12" {
		t.Fatalf("answer=%+v", answer)
	}
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatalf("rejected command ran: %v", err)
	}
}
