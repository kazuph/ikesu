package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestScriptFileDecisionWithRealSession(t *testing.T) {
	t.Parallel()
	for _, agent := range []string{"codex", "claude"} {
		for _, choice := range []string{"approve", "reject", "expired"} {
			t.Run(agent+"/"+choice, func(t *testing.T) {
				cfg := filepath.Join(t.TempDir(), "config.toml")
				timeout := "300"
				if choice == "expired" {
					timeout = "1"
				}
				if e := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[guard]\nask_timeout_seconds="+timeout+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
				s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
				s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
				s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "script-file-test", Agent: agent, State: "idle"}, nil)
				root := s.stateDir
				before := filepath.Join(root, "before")
				after := filepath.Join(root, "after")
				os.Mkdir(filepath.Join(root, "home"), 0700)
				target := root + "/home/../victim"
				os.WriteFile(target, []byte("disposable"), 0600)
				script := filepath.Join(root, "program.sh")
				body := "printf before > '" + before + "'\n/bin/rm '" + target + "'\nprintf after > '" + after + "'\n"
				if e := os.WriteFile(script, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
				line := "/bin/bash '" + script + "'"
				env := []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/zsh", "IKESU_CONFIG=" + cfg, "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "IKESU_GUARD_EXECUTABLE=" + binPath, "BASH_ENV=" + filepath.Join(root, "guard", "shell", "p_1", "bash-env"), "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesuの隔離試験。必要な理由: 承認と却下と期限切れを検証。実施内容: 使い捨て対象の削除。"}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				var cmd *exec.Cmd
				if agent == "claude" {
					cmd = exec.CommandContext(ctx, binPath, "--session", s.name, "guard", "claude-prefix", "--", line)
				} else {
					cmd = exec.CommandContext(ctx, "/bin/bash", "-c", line)
				}
				cmd.Env = env
				cmd.Dir = root
				var out bytes.Buffer
				cmd.Stdout = &out
				cmd.Stderr = &out
				if e := cmd.Start(); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				var pending api.DecisionListResult
				s.waitFor("script approval", 5*time.Second, func(string) bool {
					s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
					return len(pending.Decisions) == 1
				})
				d := pending.Decisions[0]
				if d.Body == nil || !strings.Contains(*d.Body, "の2行目:") {
					t.Fatalf("source missing: %+v", d)
				}
				if _, e := os.Stat(before); !os.IsNotExist(e) {
					t.Fatal("body ran before answer")
				}
				if dir := os.Getenv("SCRIPT_FILE_EVIDENCE_DIR"); dir != "" {
					b, _ := json.MarshalIndent(d, "", "  ")
					if e := os.WriteFile(filepath.Join(dir, agent+"-"+choice+".json"), b, 0600); e != nil {
						t.Fatal(e)
					}
				}
				if choice != "expired" {
					if raw, e := s.cli("decision", "answer", d.DecisionID, choice); e != nil {
						t.Fatal(raw, e)
					}
				}
				if choice == "approve" {
					// The outer bash grant must not authorize the inner rm. Its independent
					// DEBUG check asks again, after the first line has actually executed.
					s.waitFor("inner approval", 5*time.Second, func(string) bool {
						s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
						return len(pending.Decisions) == 1
					})
					if _, e := os.Stat(before); e != nil {
						grants, _ := os.ReadFile(filepath.Join(root, ".ai_guard_temp_approval"))
						t.Logf("grants: %s", grants)
						t.Fatalf("first line did not run: %v; next decision: %+v body=%s", e, pending.Decisions[0], *pending.Decisions[0].Body)
					}
					if raw, e := s.cli("decision", "answer", pending.Decisions[0].DecisionID, "approve"); e != nil {
						t.Fatal(raw, e)
					}
				}

				select {
				case e := <-done:
					if choice == "approve" {
						if e != nil {
							t.Fatal(e, out.String())
						}
						if _, e = os.Stat(after); e != nil {
							t.Fatal(e)
						}
						if _, e = os.Stat(target); !os.IsNotExist(e) {
							t.Fatal("approved deletion absent")
						}
					} else {
						if e == nil {
							t.Fatal("denied command executed")
						}
						if _, e = os.Stat(before); !os.IsNotExist(e) {
							t.Fatal("denied script started")
						}
						if _, e = os.Stat(target); e != nil {
							t.Fatal(e)
						}
						if choice == "expired" && !strings.Contains(out.String(), "期限切れ") {
							t.Fatal(out.String())
						}
					}
					t.Logf("agent=%s choice=%s output=%s", agent, choice, out.String())
				case <-time.After(10 * time.Second):
					t.Fatal("guard did not finish")
				}
			})
		}
	}
}
