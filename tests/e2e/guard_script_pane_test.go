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

func TestScriptFileInsideRealPane(t *testing.T) {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	for _, agent := range []string{"codex", "claude"} {
		for _, scenario := range []string{"safe", "danger", "unresolved", "block"} {
			t.Run(agent+"/"+scenario, func(t *testing.T) {
				s := start(t)
				s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
				root := s.stateDir
				before := filepath.Join(root, "before")
				after := filepath.Join(root, "after")
				os.Mkdir(filepath.Join(root, "home"), 0700)
				target := root + "/home/../victim"
				os.WriteFile(target, []byte("disposable"), 0600)
				script := filepath.Join(root, "program.sh")
				body := "printf before > " + quote(before) + "\n"
				switch scenario {
				case "danger":
					body += "/bin/rm " + quote(target) + "\n"
				case "unresolved":
					body += "TARGET=" + quote(target) + "\n/bin/rm \"$TARGET\"\n"
				case "block":
					body += "git reset --hard\n"
				}
				body += "printf after > " + quote(after) + "\n"
				if e := os.WriteFile(script, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
				line := "/bin/bash " + quote(script)
				runner := "/bin/bash"
				if agent == "claude" {
					runner = "/bin/sh"
					line = quote(binPath) + " guard claude-prefix -- " + quote(line)
				}
				// Enter a real pane shell before reporting the agent. Its read waits for
				// activation, so no real AI runtime or user's running server is touched.
				launcher := "printf 'READY_%s\\n' WAIT; read GATE; " + line
				s.send("AI_GUARD_REQUEST_REASON=" + quote("対象プロジェクト: ikesu隔離試験。必要な理由: 実ペインで方式Cを確認。実施内容: 使い捨てスクリプトの実行。") + " " + runner + " -c " + quote(launcher) + "; printf 'FINISHED_%s\\n' done\r")
				s.waitFor("runner ready", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_WAIT") })
				s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "script-pane-test", Agent: agent, State: "idle"}, nil)
				s.send("go\r")
				if scenario == "danger" || scenario == "unresolved" {
					var pending api.DecisionListResult
					s.waitFor("pane decision", 5*time.Second, func(string) bool {
						s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
						return len(pending.Decisions) == 1
					})
					d := pending.Decisions[0]
					_, e := os.Stat(before)
					if scenario == "danger" {
						if !os.IsNotExist(e) {
							t.Fatal("preflight executed first line")
						}
						if d.Body == nil || !strings.Contains(*d.Body, "の2行目:") {
							t.Fatalf("source missing: %+v", d)
						}
					} else if e != nil {
						t.Fatal("runtime guard stopped before the first line", e)
					}
					if raw, e := s.cli("decision", "answer", d.DecisionID, "reject"); e != nil {
						t.Fatal(raw, e)
					}
					t.Logf("%s/%s decision=%s body=%s", agent, scenario, d.DecisionID, *d.Body)
				}
				s.waitFor("script finished", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "FINISHED_done") })
				_, e := os.Stat(after)
				if scenario == "safe" {
					if e != nil {
						t.Fatal(e)
					}
				} else if !os.IsNotExist(e) {
					t.Fatal("after line executed")
				}
				if scenario == "block" {
					if _, e := os.Stat(before); !os.IsNotExist(e) {
						t.Fatal("blocked script started")
					}
					var pending api.DecisionListResult
					s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
					if len(pending.Decisions) != 0 {
						t.Fatal("hard block created approval")
					}
				}
				if _, e := os.Stat(target); e != nil {
					t.Fatal("disposable victim was changed", e)
				}
				if dir := os.Getenv("SCRIPT_FILE_EVIDENCE_DIR"); dir != "" {
					if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("pane-%s-%s.txt", agent, scenario)), []byte(s.text()), 0600); e != nil {
						t.Fatal(e)
					}
				}
				t.Logf("actual pane %s/%s: files and terminal output verified", agent, scenario)
			})
		}
	}
}
