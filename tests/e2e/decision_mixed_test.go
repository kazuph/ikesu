//go:build mixedversions

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestDecisionMixedVersionsNoBreak(t *testing.T) {
	old := os.Getenv("NODIALOG_BASELINE_BIN")
	if old == "" || filepath.Base(old) != "ikesu" {
		t.Fatal("NODIALOG_BASELINE_BIN must point to the original binary named ikesu")
	}
	for _, pair := range []struct {
		name, client, server string
		dialog               bool
	}{
		{"new-client-old-server", binPath, old, false},
		{"old-client-new-server", old, binPath, true},
	} {
		t.Run(pair.name, func(t *testing.T) {
			s := startVersionMode(t, nil, false, pair.client, pair.server)
			s.waitFor("shell", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			var created api.DecisionResult
			s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "MixedVersionPending", Options: []api.DecisionOption{{ID: "yes", Label: "MixedApprove", Role: "approve"}}}, &created)
			s.send("printf 'MIX_%s\\n' INPUT\r")
			s.waitFor("input while pending", 5*time.Second, func(tx string) bool {
				return strings.Contains(tx, "MIX_INPUT") && strings.Contains(tx, "MixedVersionPending") == pair.dialog
			})
			var pending api.DecisionResult
			s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: created.Decision.DecisionID}, &pending)
			if pending.Decision.Status != "pending" {
				t.Fatal("pane Enter answered the decision", pending)
			}
			if dir := os.Getenv("NODIALOG_EVIDENCE_DIR"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, pair.name+"-go-pending.screen.txt"), []byte(s.text()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := s.cli("decision", "answer", created.Decision.DecisionID, "yes"); err != nil {
				t.Fatal(out, err)
			}
			s.send("printf 'MIX_%s\\n' ANSWERED\r")
			s.waitFor("resolved and input preserved", 5*time.Second, func(tx string) bool {
				return strings.Contains(tx, "MIX_ANSWERED") && !strings.Contains(tx, "MixedVersionPending") && !strings.Contains(tx, "! 1")
			})
			s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: created.Decision.DecisionID}, &pending)
			if pending.Decision.Status != "answered" || pending.Decision.Answer == nil || pending.Decision.Answer.OptionID == nil || *pending.Decision.Answer.OptionID != "yes" {
				t.Fatal(pending)
			}
		})
	}
}
