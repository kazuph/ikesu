package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestClaudeEvalInsideRealPane(t *testing.T) {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	reason := "対象プロジェクト: ikesu隔離試験。必要な理由: 判断待ちの件数を確認する。実施内容: 送信を却下する。確認済み: 隔離サーバー。戻し方: 変更なし。"
	for _, shell := range []string{"bash", "zsh"} {
		for _, scenario := range []string{"safe", "ask"} {
			t.Run(shell+"/"+scenario, func(t *testing.T) {
				s := start(t)
				s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
				payload := "pwd"
				if scenario == "ask" {
					payload = "AI_GUARD_REQUEST_REASON=" + q(reason) + " dd if=/dev/null of=/dev/null"
				}
				wrapper := "{ shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \\builtin unalias -- 'which'; \\builtin unset -f -- 'which'; } >/dev/null 2>&1 || true && { \\builtin unalias -- 'unsetenv'; \\builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval " + q(payload) + " < /dev/null && pwd -P >| " + q(s.stateDir+"/claude-test-cwd")
				startup := s.stateDir + "/guard/shell/p_1"
				launcher := "printf 'READY_%s\\n' WAIT; read GATE; exec /bin/" + shell + " -c " + q(wrapper)
				s.send("BASH_ENV=" + q(startup+"/bash-env") + " ZDOTDIR=" + q(startup) + " /bin/sh -c " + q(launcher) + "; printf 'FINISHED_%s\\n' done\r")
				s.waitFor("runner ready", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_WAIT") })
				s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "eval-pane-test", Agent: "claude", State: "idle"}, nil)
				s.send("go\r")
				if scenario == "ask" {
					var pending api.DecisionListResult
					s.waitFor("one decision", 10*time.Second, func(string) bool {
						s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
						return len(pending.Decisions) == 1
					})
					d := pending.Decisions[0]
					if d.Origin == nil || d.Origin.Reason != reason {
						t.Fatalf("request reason missing: %+v", d.Origin)
					}
					t.Logf("isolated panel record: %s %s", d.DecisionID, *d.Body)
					if raw, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
						t.Fatal(raw, err)
					}
				}
				s.waitFor("finished", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "FINISHED_done") })
				var pending api.DecisionListResult
				s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				if len(pending.Decisions) != 0 {
					t.Fatalf("remaining decisions: %v", pending.Decisions)
				}
				var all api.DecisionListResult
				s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &all)
				want := 0
				if scenario == "ask" {
					want = 1
				}
				if len(all.Decisions) != want {
					t.Fatalf("got %d decisions, want %d", len(all.Decisions), want)
				}
				if scenario == "safe" {
					if _, err := os.Stat(s.stateDir + "/claude-test-cwd"); err != nil {
						t.Fatal("safe wrapper did not complete:", err)
					}
				}
			})
		}
	}
}
