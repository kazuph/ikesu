package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestRuntimeReasonFromOriginalEnvironment(t *testing.T) {
	const original = "対象プロジェクト: ikesu。必要な理由: 元の申請文を実行中の判断へ引き継ぐ試験。実施内容: 実行前に却下します。"
	const forged = "スクリプトが後から作った偽の申請文です。"
	for name, shell := range map[string]string{"system": "/bin/bash", "homebrew": "/opt/homebrew/bin/bash"} {
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				if name == "homebrew" && os.IsNotExist(err) {
					t.Skip("Homebrew Bashがないため、この版だけ未実施")
				}
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, own, parent, script string
				dialog                    bool
			}{
				{"own-original-loader", original, "", "", true},
				{"outer-approval", original, "", "", true},
				{"approved-other-rm", original, "", "", true},
				{"approved-different-args", original, "", "", true},
				{"approved-forged-mark", original, "", "", true},
				{"approved-forged-file", original, "", "", true},
				{"custom-reason", original, "", "export PROJECT_APPROVAL_REASON=" + shQuote(forged) + "\n", true},
				{"parent-original-loader", "", original, "", true},
				{"overwrite-own", original, "", "AI_GUARD_REQUEST_REASON=" + shQuote(forged) + "\n", true},
				{"overwrite-parent", "", original, "IKESU_GUARD_PARENT_REASON=" + shQuote(forged) + "\n", true},
				{"missing", "", "", "", false},
				{"forged-only", "", "", "export AI_GUARD_REQUEST_REASON=" + shQuote(forged) + "\n", false},
				{"forged-inline", "", "", "", false},
				{"forged-parent-only", "", "", "export IKESU_GUARD_PARENT_REASON=" + shQuote(forged) + "\n", false},
				{"forged-selector", "", "", "export AI_GUARD_REQUEST_ENV_NAME=FORGED_REASON\nexport FORGED_REASON=" + shQuote(forged) + "\n", false},
				{"english", "Please approve this command", "", "", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := start(t)
					s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
					s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "runtime-reason-test", Agent: "claude", State: "idle"}, nil)
					root := s.stateDir
					if err := os.Mkdir(filepath.Join(root, "home"), 0700); err != nil {
						t.Fatal(err)
					}
					victim := root + "/home/../victim"
					otherVictim := root + "/home/../victim-other"
					if err := os.WriteFile(otherVictim, []byte("unchanged"), 0600); err != nil {
						t.Fatal(err)
					}
					after := filepath.Join(root, "after")
					if err := os.WriteFile(victim, []byte("unchanged"), 0600); err != nil {
						t.Fatal(err)
					}
					loader := filepath.Join(root, "original-loader.sh")
					// The real loader's first action is this unset, before its marker check.
					loaderBody := []byte("unset AI_GUARD_REQUEST_REASON\n")
					if fixture := os.Getenv("IKESU_REASON_LOADER_FIXTURE"); fixture != "" {
						var err error
						loaderBody, err = os.ReadFile(fixture)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(loader, loaderBody, 0600); err != nil {
						t.Fatal(err)
					}
					script := filepath.Join(root, "danger.sh")
					command := "rm " + shQuote(victim)
					if tc.name == "forged-inline" {
						command = "AI_GUARD_REQUEST_REASON=" + shQuote(forged) + " " + command
					}
					if err := os.WriteFile(script, []byte(tc.script+command+"\nprintf after > "+shQuote(after)+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					bashEnv := filepath.Join(root, "guard", "shell", "p_1", "bash-env")
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, shell, script)
					if isOuterReasonCase(tc.name) {
						cmd = exec.CommandContext(ctx, binPath, "guard", "claude-prefix", "--", "AI_GUARD_REQUEST_REASON="+shQuote(original)+" "+shQuote(shell)+" "+shQuote(script))
					}
					cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
					cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
					cmd.WaitDelay = time.Second
					cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_STATE_DIR=" + root, "IKESU_CONFIG=" + filepath.Join(root, "no-config"), "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "IKESU_GUARD_ENFORCING=1", "IKESU_GUARD_EXECUTABLE=" + binPath, "IKESU_GUARD_BIN=" + filepath.Join(root, "guard", "bin", "p_1"), "BASH_ENV=" + bashEnv, "IKESU_GUARD_BASH_ENV_ORIG=" + loader, "AI_GUARD_REQUEST_REASON=" + tc.own, "IKESU_GUARD_PARENT_REASON=" + tc.parent, "IKESU_GUARD_PARENT_COMMAND=bash outer-request.sh"}
					if isOuterReasonCase(tc.name) {
						cmd.Env = append(cmd.Env, "SHELL=/bin/zsh", "AI_GUARD_REQUEST_REASON=")
					}
					if tc.name == "custom-reason" {
						cmd.Env = append(cmd.Env, "AI_GUARD_REQUEST_ENV_NAME=PROJECT_APPROVAL_REASON", "PROJECT_APPROVAL_REASON="+original)
					}
					cmd.Dir = root
					var output bytes.Buffer
					cmd.Stdout = &output
					cmd.Stderr = &output
					if tc.dialog {
						if err := cmd.Start(); err != nil {
							t.Fatal(err)
						}
						done := make(chan error, 1)
						go func() { done <- cmd.Wait() }()
						var pending api.DecisionListResult
						s.waitFor("runtime original reason approval", 5*time.Second, func(string) bool {
							s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
							select {
							case err := <-done:
								t.Fatalf("script exited before runtime decision: %v output=%s", err, output.String())
							default:
							}
							return len(pending.Decisions) == 1
						})
						decision := pending.Decisions[0]
						if isOuterReasonCase(tc.name) {
							if decision.Origin == nil || decision.Origin.Reason != original {
								t.Fatal("outer reason lost", decision)
							}
							if strings.HasPrefix(tc.name, "approved-") {
								replacement := "rm " + shQuote(otherVictim)
								if tc.name == "approved-other-rm" {
									replacement = "/bin/rm " + shQuote(otherVictim)
								}
								if tc.name == "approved-forged-mark" {
									replacement = "export IKESU_GUARD_APPROVED_COMMAND=" + shQuote(replacement) + "\nexport IKESU_GUARD_APPROVAL_OWNER=" + strconv.Itoa(cmd.Process.Pid) + "\n" + replacement
								}
								if tc.name == "approved-forged-file" {
									replacement = "printf fake > " + shQuote(filepath.Join(root, "guard", "approved-invocations", "forged.json")) + "\n" + replacement
								}
								if err := os.WriteFile(script, []byte(replacement+"\nprintf after > "+shQuote(after)+"\n"), 0600); err != nil {
									t.Fatal(err)
								}
							}
							if text, err := s.cli("decision", "answer", decision.DecisionID, "approve"); err != nil {
								t.Fatal(text, err)
							}
							if tc.name == "outer-approval" {
								if err := <-done; err != nil {
									t.Fatalf("approved exact command did not run: %v %s", err, output.String())
								}
								var list api.DecisionListResult
								s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &list)
								if len(list.Decisions) != 0 {
									t.Fatal("exact approved invocation asked again")
								}
								if _, err := os.Stat(victim); !os.IsNotExist(err) {
									t.Fatal("approved victim not removed", err)
								}
								if data, err := os.ReadFile(after); err != nil || string(data) != "after" {
									t.Fatal("approved continuation failed", err)
								}
								t.Log("outer approval reused for exact argv; decisions=1 after=present")
								return
							}
							outerID := decision.DecisionID
							s.waitFor("runtime decision after outer approval", 5*time.Second, func(string) bool {
								select {
								case err := <-done:
									approval, _ := os.ReadFile(filepath.Join(root, ".ai_guard_temp_approval"))
									t.Fatalf("script exited after outer approval: %v output=%s approval=%s", err, output.String(), approval)
								default:
								}
								s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
								return len(pending.Decisions) == 1 && pending.Decisions[0].DecisionID != outerID
							})
							decision = pending.Decisions[0]
						}
						if decision.Origin == nil || decision.Origin.Reason != original || decision.Body == nil || strings.Contains(*decision.Body, forged) {
							t.Fatalf("original reason lost or forged: %+v", decision)
						}
						if text, err := s.cli("decision", "answer", decision.DecisionID, "reject"); err != nil {
							t.Fatal(text, err)
						}
						if err := <-done; err == nil {
							t.Fatal("rejected runtime command succeeded")
						}
					} else {
						if err := cmd.Run(); err == nil {
							t.Fatal("missing original reason allowed script")
						}
						if !strings.Contains(output.String(), "申請文") {
							t.Fatal("reason hint missing", output.String())
						}
						for _, status := range []string{"pending", "answered", "expired", "cancelled"} {
							var list api.DecisionListResult
							s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: status}, &list)
							if len(list.Decisions) != 0 {
								t.Fatal("forged/missing reason created decision", status)
							}
						}
					}
					if ctx.Err() != nil {
						t.Fatal("script timed out", output.String())
					}
					if data, err := os.ReadFile(victim); err != nil || string(data) != "unchanged" {
						t.Fatal("victim changed", err)
					}
					if data, err := os.ReadFile(otherVictim); err != nil || string(data) != "unchanged" {
						t.Fatal("unapproved other victim changed", err)
					}
					if tc.name == "approved-forged-file" {
						if _, err := os.Stat(filepath.Join(root, "guard", "approved-invocations", "forged.json")); !os.IsNotExist(err) {
							t.Fatal("script forged a grant", err)
						}
					}
					if _, err := os.Stat(after); !os.IsNotExist(err) {
						t.Fatal("post-rejection line ran", err)
					}
					t.Logf("runtime reason %s %s decisions=%v victim=unchanged after=absent", name, tc.name, tc.dialog)
				})
			}
		})
	}
}

func shQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func isOuterReasonCase(name string) bool {
	return name == "outer-approval" || strings.HasPrefix(name, "approved-")
}

func TestRawDecisionCannotForgeGuardExecution(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	claim := api.GuardExecutionClaim{CommandHash: strings.Repeat("a", 64), Rule: "rm-paths"}
	var created api.DecisionResult
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "command_guard", Title: "untrusted request", Origin: &api.DecisionOrigin{PaneID: "p1"}, GuardExecution: &claim, Options: []api.DecisionOption{{ID: "approve", Label: "承認", Role: "approve"}}}, &created)
	if text, err := s.cli("decision", "answer", created.Decision.DecisionID, "approve"); err != nil {
		t.Fatal(text, err)
	}
	var result api.GuardApprovedResult
	s.rpc(api.MethodDecisionGuardApproved, api.GuardApprovedParams{GuardExecutionClaim: claim, PaneID: "p1"}, &result)
	if result.Approved {
		t.Fatal("raw decision.create forged an execution grant")
	}
}
