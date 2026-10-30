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

func TestClaudeGitWrapperRealSession(t *testing.T) {
	data, err := os.ReadFile("../../internal/guard/testdata/claude-git-wrapper.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		FullLine string `json:"full_line"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	payload := `git -C /Users/kazuph/src/github.com/kazuph/ikesu grep -n "func claudeLiteral" -A22 -- internal/guard | head -30`
	line := strings.Replace(fixture.FullLine, payload, "git --version", 1)
	if line == fixture.FullLine {
		t.Fatal("observed payload missing")
	}
	f, err := os.CreateTemp("/tmp", "claude-*-cwd")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	line = strings.Replace(line, "/tmp/claude-c2be-cwd", f.Name(), 1)
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "git-prefix-test", Agent: "claude", State: "idle"}, nil)
	root := s.stateDir
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--session", s.name, "guard", "claude-prefix", "--", line)
	cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/zsh", "IKESU_CONFIG=" + filepath.Join(root, "no-config"), "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "BASH_ENV=" + filepath.Join(root, "guard", "shell", "p_1", "bash-env")}
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "git version") {
		t.Fatalf("git --version: %v %s", err, out)
	}
	var decisions api.DecisionListResult
	s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &decisions)
	if len(decisions.Decisions) != 0 {
		t.Fatalf("unexpected decisions: %+v", decisions.Decisions)
	}
}

func TestClaudeEvalPrefixRealSession(t *testing.T) {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	reason := "対象プロジェクト: ikesu隔離試験。必要な理由: 判断待ちを確認する。実施内容: 試験操作を却下する。確認済み: 隔離サーバー。戻し方: 変更なし。"
	for _, scenario := range []string{"safe", "ask"} {
		t.Run(scenario, func(t *testing.T) {
			s := start(t)
			s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "eval-prefix-test", Agent: "claude", State: "idle"}, nil)
			root := s.stateDir
			payload := "pwd"
			if scenario == "ask" {
				payload = "AI_GUARD_REQUEST_REASON=" + quote(reason) + " dd if=/dev/null of=/dev/null"
			}
			line := "{ shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \\builtin unalias -- 'which'; \\builtin unset -f -- 'which'; } >/dev/null 2>&1 || true && { \\builtin unalias -- 'unsetenv'; \\builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval " + quote(payload) + " < /dev/null && pwd -P >| " + quote(root+"/claude-test-cwd")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "--session", s.name, "guard", "claude-prefix", "--", line)
			cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/zsh", "IKESU_CONFIG=" + filepath.Join(root, "no-config"), "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "BASH_ENV=" + filepath.Join(root, "guard", "shell", "p_1", "bash-env")}
			cmd.Dir = root
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			if scenario == "ask" {
				var pending api.DecisionListResult
				s.waitFor("one decision", 10*time.Second, func(string) bool {
					s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
					return len(pending.Decisions) == 1
				})
				d := pending.Decisions[0]
				if d.Origin == nil || d.Origin.Reason != reason {
					t.Fatalf("reason missing: %+v", d.Origin)
				}
				if raw, err := s.cli("decision", "answer", d.DecisionID, "reject"); err != nil {
					t.Fatal(raw, err)
				}
			}
			err := <-done
			if scenario == "safe" && err != nil {
				t.Fatalf("safe failed: %v %s", err, output.String())
			}
			if scenario == "ask" && err == nil {
				t.Fatal("rejected command succeeded")
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
				if _, err := os.Stat(root + "/claude-test-cwd"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
