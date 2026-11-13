package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// SPEC-line-approval.md 6章 (c): 行の承認が、別の段（シム・関数・DEBUG）の二重の確認を無くすことの通しの試験。
// 期待は、人への確認の回数。

type lineSession struct {
	s    *session
	cfg  string
	root string
	path string
}

func newLineSession(t *testing.T, shim bool) *lineSession {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if e := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[guard]\nask_timeout_seconds=300\n"), 0600); e != nil {
		t.Fatal(e)
	}
	// 何もしない偽の aws・curl・gh を、ペインの PATH に先に置く（シムは PATH にある実行ファイルに作られる）。
	fake := filepath.Join(t.TempDir(), "fakebin")
	if e := os.MkdirAll(fake, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"aws", "curl", "gh"} {
		if e := os.WriteFile(filepath.Join(fake, name), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg, "PATH": fake + ":" + os.Getenv("PATH")})
	s.waitFor("prompt", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "line-approval-test", Agent: "claude", State: "idle"}, nil)
	root := s.stateDir
	path := fake + ":/usr/bin:/bin"
	if shim {
		path = filepath.Join(root, "guard", "bin", "p_1") + ":" + path
	}
	return &lineSession{s: s, cfg: cfg, root: root, path: path}
}

// run は agent の入口（claude は claude-prefix、codex は bash -c）で行を実行し、出た確認を全部承認して、確認の件数を返す。
func (l *lineSession) run(t *testing.T, agent, line string) (asked int, out string) {
	t.Helper()
	s := l.s
	env := []string{"HOME=" + l.root, "PATH=" + l.path, "SHELL=/bin/zsh", "IKESU_CONFIG=" + l.cfg, "IKESU_STATE_DIR=" + l.root, "IKESU_SOCKET_PATH=" + api.APISocketPath(filepath.Join(s.stateDir, s.name)), "IKESU_SOCKET_PATH_EXPLICIT=1", "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(l.root, "guard", "agent-panes", "p_1.json"), "IKESU_GUARD_EXECUTABLE=" + binPath, "BASH_ENV=" + filepath.Join(l.root, "guard", "shell", "p_1", "bash-env"), "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesuの隔離試験。必要な理由: 行の承認の回数を検証。実施内容: 何もしない偽のコマンドを実行。"}
	before := l.count(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if agent == "claude" {
		cmd = exec.CommandContext(ctx, binPath, "--session", s.name, "guard", "claude-prefix", "--", line)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/bash", "-c", line)
	}
	cmd.Env, cmd.Dir = env, l.root
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case e := <-done:
			if e != nil {
				t.Fatalf("%s %q failed: %v\n%s", agent, line, e, buf.String())
			}
			return l.count(t) - before, buf.String()
		case <-time.After(150 * time.Millisecond):
			var pending api.DecisionListResult
			s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
			for _, d := range pending.Decisions {
				if raw, e := s.cli("decision", "answer", d.DecisionID, "approve"); e != nil {
					t.Fatal(raw, e)
				}
			}
		}
	}
}

func (l *lineSession) count(t *testing.T) int {
	var all api.DecisionListResult
	l.s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &all)
	return len(all.Decisions)
}

// L3 用の許可のファイルは作らない（temp-approvals に L3 のキーも無い）。
func (l *lineSession) noL3GrantFiles(t *testing.T) {
	t.Helper()
	if _, e := os.Stat(filepath.Join(l.root, "guard", "one-shot-approvals")); !os.IsNotExist(e) {
		t.Errorf("one-shot approval file exists: %v", e)
	}
	if b, e := os.ReadFile(filepath.Join(l.root, "guard", "temp-approvals")); e == nil && strings.TrimSpace(string(b)) != "" {
		t.Errorf("temp-approvals has entries for L3 commands: %q", b)
	}
}

func TestLineApprovalAsksOncePerLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, agent, line string
		shim              bool
		want              int
		setup             func(root string)
	}{
		{"shim-create-table", "codex", "command aws dynamodb create-table --table-name t", true, 1, nil},
		{"function-create-table", "codex", "aws dynamodb create-table --table-name t", true, 1, nil},
		{"two-commands", "codex", "curl -X DELETE https://example.com/a; gh repo delete x/y --yes", true, 1, nil},
		{"background-pair", "codex", "aws s3 rb s3://b & aws s3 rb s3://b; wait", true, 1, nil},
		{"pipe", "codex", "aws s3 ls | head", true, 1, nil},
		{"subshell", "codex", "(aws s3 rb s3://b)", true, 1, nil},
		{"subshell-cd", "codex", "(cd /tmp && aws s3 rb s3://b)", true, 2, nil},
		{"unresolved-variable", "codex", `B=$(echo b); aws s3 rb "s3://$B"`, true, 2, nil},
		{"script", "codex", "/bin/bash x.sh", true, 1, func(root string) {
			os.WriteFile(filepath.Join(root, "x.sh"), []byte("aws s3 rb s3://b\n"), 0600)
		}},
		{"script-runtime-value", "codex", "/bin/bash y.sh v", true, 2, func(root string) {
			os.WriteFile(filepath.Join(root, "y.sh"), []byte("B=$1\naws s3 rb \"s3://$B\"\n"), 0600)
		}},
		{"script-other-command-at-runtime", "codex", "/bin/bash z.sh", true, 2, func(root string) {
			os.WriteFile(filepath.Join(root, "z.sh"), []byte("aws dynamodb create-table --table-name t\nop=delete-table\naws dynamodb $op --table-name t\n"), 0600)
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := newLineSession(t, tc.shim)
			if tc.setup != nil {
				tc.setup(l.root)
			}
			asked, out := l.run(t, tc.agent, tc.line)
			if asked != tc.want {
				var all api.DecisionListResult
				l.s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &all)
				var who []string
				for _, d := range all.Decisions {
					who = append(who, d.Title+" / "+d.Origin.Command+" / "+d.Origin.GuardRule)
				}
				log, _ := os.ReadFile(filepath.Join(l.root, "guard", "decisions.jsonl"))
				t.Fatalf("%q asked %d times, want %d\n%s\n%s\n%s", tc.line, asked, tc.want, out, strings.Join(who, "\n"), log)
			}
			l.noL3GrantFiles(t)
			if os.Getenv("LINE_APPROVAL_LOG") != "" {
				log, _ := os.ReadFile(filepath.Join(l.root, "guard", "decisions.jsonl"))
				t.Logf("decisions.jsonl:\n%s", log)
				entries, _ := os.ReadDir(filepath.Join(l.root, "guard", "bin", "p_1"))
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Logf("shims: %v PATH=%s", names, l.path)
			}
		})
	}
}

// 承認した行とは別に、あとから打った同じコマンドは聞く。
func TestLineApprovalDoesNotCarryToNextCall(t *testing.T) {
	t.Parallel()
	l := newLineSession(t, true)
	for n := 1; n <= 2; n++ {
		asked, out := l.run(t, "codex", "aws dynamodb create-table --table-name t")
		if asked != 1 {
			t.Fatalf("call %d asked %d times, want 1\n%s", n, asked, out)
		}
	}
	l.noL3GrantFiles(t)
}

// 対話の zsh（accept-line）で承認した行の、関数・シムは聞かない。次の行で同じコマンドを打つと聞く。
func TestLineApprovalInteractiveZshNextLineAsksAgain(t *testing.T) {
	t.Parallel()
	s := start(t)
	s.waitFor("shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "line-approval-test", Agent: "claude", State: "idle"}, nil)
	dir := t.TempDir()
	if e := os.Mkdir(filepath.Join(dir, "home"), 0700); e != nil {
		t.Fatal(e)
	}
	reason := "対象プロジェクト: ikesuの隔離試験。必要な理由: 行の承認の回数を検証。実施内容: 使い捨て対象の削除。"
	for round := 1; round <= 2; round++ {
		target := filepath.Join(dir, "home") + "/../victim"
		if e := os.WriteFile(target, []byte("test-only"), 0600); e != nil {
			t.Fatal(e)
		}
		status := filepath.Join(dir, fmt.Sprintf("status-%d", round))
		s.send(fmt.Sprintf("AI_GUARD_REQUEST_REASON=%s rm %s; printf '%%s' $? > %s\r", shQuote(reason), shQuote(target), shQuote(status)))
		var pending api.DecisionListResult
		s.waitFor(fmt.Sprintf("ask %d", round), 10*time.Second, func(string) bool {
			s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
			return len(pending.Decisions) == 1
		})
		if out, e := s.cli("decision", "answer", pending.Decisions[0].DecisionID, "approve"); e != nil {
			t.Fatal(e, out)
		}
		// 関数・シムの2段目は聞かずに実行が終わる（ask がもう1件出たら終わらない）。
		s.waitFor(fmt.Sprintf("round %d done", round), 10*time.Second, func(string) bool { _, e := os.Stat(status); return e == nil })
		if b, _ := os.ReadFile(status); string(b) != "0" {
			t.Fatalf("round %d status=%q", round, b)
		}
		if _, e := os.Stat(target); !os.IsNotExist(e) {
			t.Fatalf("round %d: target still exists", round)
		}
	}
	var all api.DecisionListResult
	s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &all)
	if len(all.Decisions) != 2 {
		t.Fatalf("asked %d times, want 2 (once per line)", len(all.Decisions))
	}
}

// 行が終わった後に残ったバックグラウンドの子が、同じ L3 を走らせたら聞く（持ち主が終わると承認も終わる）。
func TestLineApprovalBackgroundChildAsksAfterLineEnds(t *testing.T) {
	t.Parallel()
	l := newLineSession(t, true)
	asked, out := l.run(t, "codex", "aws s3 rb s3://z; (sleep 2; aws s3 rb s3://z) >/dev/null 2>&1 &")
	if asked != 1 {
		t.Fatalf("the line asked %d times, want 1\n%s", asked, out)
	}
	var pending api.DecisionListResult
	l.s.waitFor("background child asks", 15*time.Second, func(string) bool {
		l.s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	if raw, e := l.s.cli("decision", "answer", pending.Decisions[0].DecisionID, "approve"); e != nil {
		t.Fatal(raw, e)
	}
	l.noL3GrantFiles(t)
}
