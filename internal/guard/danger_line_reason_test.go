package guard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// A deploy/publish line asks as a whole (danger-word-line). The approval must
// carry the dangerous command's own inline request reason; before the fix the
// reason was dropped and every such line was cancelled as reasonless
// (2026-11-12, `AI_GUARD_REQUEST_REASON='…' npx wrangler deploy`).
func TestDangerWordLineKeepsInlineRequestReason(t *testing.T) {
	t.Parallel()
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": os.Getenv("HOME"), "PATH": os.Getenv("PATH")}}
	claude := strings.Replace(strings.TrimPrefix(claudeBootstrap, "source IKESU_SNAPSHOT 2>/dev/null || true && "),
		"IKESU_PAYLOAD", `'AI_GUARD_REQUEST_REASON='"'"'本番に置くため'"'"' npx wrangler deploy'`, 1)
	claude = strings.Replace(claude, "IKESU_CWD", "/tmp/claude-ab12-cwd", 1)
	for _, tc := range []struct{ name, line, want string }{
		{"inline", "AI_GUARD_REQUEST_REASON='本番に置くため' npx --prefix /x wrangler deploy -c /x/w.jsonc", "本番に置くため"},
		{"after cd", "cd /tmp && AI_GUARD_REQUEST_REASON='本番に置くため' npx wrangler deploy", "本番に置くため"},
		{"inside eval", `eval 'AI_GUARD_REQUEST_REASON='"'"'本番に置くため'"'"' npx wrangler deploy'`, "本番に置くため"},
		{"claude bootstrap", ClaudePrefixLine(i, claude), "本番に置くため"},
		// The reason belongs to the command it is written on, not to a neighbour.
		{"reason on another command", "AI_GUARD_REQUEST_REASON='別の理由' echo hi; npx wrangler deploy", ""},
	} {
		out, v, err := lineVerdict(Config{}, i, tc.line)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if v.Decision != "ask" || v.Rule != "danger-word-line" {
			t.Fatalf("%s: verdict %+v, want ask danger-word-line", tc.name, v)
		}
		if got := requestReason(out); got != tc.want {
			t.Errorf("%s: request reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Only the request reason moves to the line's approval. Other inline
// assignments on the deploy command, such as a temporary approval file the
// AI wrote itself, keep the line's own values.
func TestDangerWordLineDoesNotTakeOtherInlineSettings(t *testing.T) {
	t.Parallel()
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": os.Getenv("HOME"), "PATH": os.Getenv("PATH")}}
	line := "AI_GUARD_TEMP_APPROVAL_FILE=/tmp/ai-wrote-this AI_GUARD_TEMP_APPROVAL_SECONDS=999999 IKESU_GUARD_MARKER=x " +
		"AI_GUARD_REQUEST_REASON='本番に置くため' npx wrangler deploy"
	out, v, err := lineVerdict(Config{}, i, line)
	if err != nil || v.Decision != "ask" || v.Rule != "danger-word-line" {
		t.Fatalf("verdict %+v err %v", v, err)
	}
	if got := requestReason(out); got != "本番に置くため" {
		t.Fatalf("request reason = %q", got)
	}
	for _, key := range []string{"AI_GUARD_TEMP_APPROVAL_FILE", "AI_GUARD_TEMP_APPROVAL_SECONDS", "IKESU_GUARD_MARKER"} {
		if value, ok := out.Env[key]; ok {
			t.Errorf("Env took the command's %s=%q", key, value)
		}
		if value, ok := out.AuthorizationEnv[key]; ok {
			t.Errorf("AuthorizationEnv took the command's %s=%q", key, value)
		}
	}
}

// With several deploy/publish commands on one line, a reason is passed only
// when every one of them carries its own.
func TestDangerWordLineNeedsAReasonOnEveryDangerousCommand(t *testing.T) {
	t.Parallel()
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": os.Getenv("HOME"), "PATH": os.Getenv("PATH")}}
	for _, tc := range []struct{ name, line, want string }{
		{"second lacks a reason", "AI_GUARD_REQUEST_REASON='一つ目' npx a deploy && npx b publish", ""},
		{"first lacks a reason", "npx a deploy; AI_GUARD_REQUEST_REASON='二つ目' npx b publish", ""},
		{"both carry one", "AI_GUARD_REQUEST_REASON='一つ目' npx a deploy && AI_GUARD_REQUEST_REASON='二つ目' npx b publish", "一つ目"},
	} {
		out, v, err := lineVerdict(Config{}, i, tc.line)
		if err != nil || v.Rule != "danger-word-line" {
			t.Fatalf("%s: verdict %+v err %v", tc.name, v, err)
		}
		if got := requestReason(out); got != tc.want {
			t.Errorf("%s: request reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDangerWordLineKeepsInlineRequestReasonOnObservedClaudeBootstraps(t *testing.T) {
	t.Parallel()
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": os.Getenv("HOME"), "PATH": os.Getenv("PATH")}}
	payload := "AI_GUARD_REQUEST_REASON='確認画面が出るかの試験' echo deploy-test"
	optBlocks := []struct{ name, block string }{
		{"legacy", `{ shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; }`},
		{"builtin_no_equals", `{ \builtin shopt -u extglob || \builtin setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_BRACE_CCL NO_MAGIC_EQUAL_SUBST; }`},
		{"builtin_with_equals", `{ \builtin shopt -u extglob || \builtin setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_BRACE_CCL NO_MAGIC_EQUAL_SUBST NO_EQUALS; }`},
		// Observed 2026-11-25: Claude Code added NO_RC_QUOTES before NO_EQUALS.
		{"builtin_with_rc_quotes", `{ \builtin shopt -u extglob || \builtin setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_BRACE_CCL NO_MAGIC_EQUAL_SUBST NO_RC_QUOTES NO_EQUALS; }`},
	}
	bashCheck := `{ case "${BASH_VERSINFO-}" in '2'|'3') \builtin printf '%s\n' '__CLAUDE_CODE_DID_NOT_RUN_BASH_TOO_OLD='"${BASH_VERSION-}" >&2; \builtin exit 126; \exit 126;; esac; } && `
	for _, opt := range optBlocks {
		for _, withBashCheck := range []bool{false, true} {
			for _, withWhich := range []bool{false, true} {
				line := opt.block + ` >/dev/null 2>&1 || true && `
				if withBashCheck {
					line = bashCheck + line
				}
				if withWhich {
					line += claudeWhich
				}
				line += `{ \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval ` + quote(payload) + ` < /dev/null && pwd -P >| /tmp/claude-8398-cwd`
				got := ClaudePrefixLine(i, line)
				if got != payload {
					t.Errorf("%s bashCheck=%v which=%v: ClaudePrefixLine did not extract payload:\ngot:  %q\nwant: %q", opt.name, withBashCheck, withWhich, got, payload)
					continue
				}
				out, v, err := lineVerdict(Config{}, i, got)
				if err != nil || v.Decision != "ask" || v.Rule != "danger-word-line" {
					t.Errorf("%s bashCheck=%v which=%v: verdict %+v err %v", opt.name, withBashCheck, withWhich, v, err)
					continue
				}
				if reason := requestReason(out); reason != "確認画面が出るかの試験" {
					t.Errorf("%s bashCheck=%v which=%v: request reason = %q", opt.name, withBashCheck, withWhich, reason)
				}
			}
		}
	}
	// Altered bootstrap must still be rejected.
	altered := `{ \builtin shopt -u extglob || \builtin setopt EXTENDED_GLOB; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval ` + quote(payload) + ` < /dev/null && pwd -P >| /tmp/claude-8398-cwd`
	if got := ClaudePrefixLine(i, altered); got != altered {
		t.Fatalf("altered bootstrap unexpectedly normalized to %q", got)
	}
}

func TestClaudePrefixExecStringPassesThroughAndPreservesSecurity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cwd := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	mode := "enforce"
	cfg := Config{Mode: mode}
	a := NewAssets(root, "p_22", exe, cfg)
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p22", "IKESU_SOCKET_PATH=/tmp/s.sock"}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Sync("claude"); err != nil {
		t.Fatal(err)
	}
	prefixPath := filepath.Join(root, "guard", "shell", "p_22", "claude-prefix")
	payload := "AI_GUARD_REQUEST_REASON='確認画面が出るかの試験' bash /tmp/scripts/deploy-bp.sh"
	bootstrap := `{ \builtin shopt -u extglob || \builtin setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_BRACE_CCL NO_MAGIC_EQUAL_SUBST NO_EQUALS; } >/dev/null 2>&1 || true && { \builtin unalias -- 'which'; \builtin unset -f -- 'which'; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval ` + quote(payload) + ` < /dev/null && pwd -P >| /tmp/claude-8398-cwd`
	execLine := prefixPath + " " + quote(bootstrap)

	calledAwait := false
	await := func(p api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
		calledAwait = true
		return api.Decision{}, nil
	}

	// Outer shell exec-string / zsh-debug / bash-debug must pass the verified
	// pane claude-prefix call through to claude-prefix itself (which guards the
	// unwrapped payload) instead of cancelling with no_reason.
	for _, via := range []string{"exec-string", "zsh-debug", "bash-debug"} {
		calledAwait = false
		var errOut bytes.Buffer
		i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: via}
		if !AuthorizeLine(cfg, root, i, execLine, &errOut, await) {
			t.Fatalf("via=%s: AuthorizeLine blocked verified claude-prefix call: %s", via, errOut.String())
		}
		if calledAwait {
			t.Fatalf("via=%s: unexpectedly prompted in outer shell instead of deferring to claude-prefix", via)
		}
	}

	// Stage 2: claude-prefix unwraps the bootstrap and opens the approval dialog
	// with the inline Japanese request reason instead of cancelling as no_reason.
	var gotDecisionParams api.DecisionCreateParams
	calledAwait = false
	approve := func(p api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
		calledAwait = true
		gotDecisionParams = p
		opt := "approve"
		return api.Decision{Status: "answered", Answer: &api.DecisionAnswer{OptionID: &opt}}, nil
	}
	var stage2Err bytes.Buffer
	stage2Input := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: "claude-prefix"}
	if !AuthorizeLine(cfg, root, stage2Input, ClaudePrefixLine(stage2Input, bootstrap), &stage2Err, approve) {
		t.Fatalf("stage2 claude-prefix failed: %s", stage2Err.String())
	}
	if !calledAwait {
		t.Fatal("stage2 claude-prefix did not open approval dialog")
	}
	if gotDecisionParams.Origin == nil || gotDecisionParams.Origin.GuardRule != "danger-word-line" || gotDecisionParams.Origin.Reason != "確認画面が出るかの試験" {
		t.Fatalf("stage2 origin = %+v", gotDecisionParams.Origin)
	}
	// Rejection in Stage 2 must deny execution.
	reject := func(p api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
		opt := "reject"
		return api.Decision{Status: "answered", Answer: &api.DecisionAnswer{OptionID: &opt}}, nil
	}
	var rejectErr bytes.Buffer
	if AuthorizeLine(cfg, root, stage2Input, ClaudePrefixLine(stage2Input, bootstrap), &rejectErr, reject) {
		t.Fatal("stage2 claude-prefix unexpectedly allowed after reject")
	}

	// Security boundaries: non-exec/debug via, non-bootstrap/harness args,
	// extra commands, redirs, assigns, command substitutions, or fake prefix
	// paths must NOT pass through.
	for _, tc := range []struct{ name, via, line string }{
		{"accept-line", "accept-line", execLine},
		{"claude-prefix", "claude-prefix", execLine},
		{"direct non-bootstrap arg", "exec-string", prefixPath + " " + quote("echo deploy")},
		{"harness-like arg", "exec-string", prefixPath + " " + quote("node ~/.claude/hooks/deploy.js")},
		{"extra command", "exec-string", execLine + "; echo deploy"},
		{"redirection", "exec-string", execLine + " > /tmp/out"},
		{"assignment", "exec-string", "FOO=1 " + execLine},
		{"cmd subst", "exec-string", prefixPath + ` "$(echo deploy)"`},
		{"fake prefix", "exec-string", "/tmp/fake/claude-prefix " + quote(bootstrap)},
	} {
		var errOut bytes.Buffer
		i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: tc.via}
		if AuthorizeLine(cfg, root, i, tc.line, &errOut, nil) {
			t.Errorf("%s: unexpectedly allowed %q", tc.name, tc.line)
		}
	}

	// Non-claude agent marker must NOT pass through.
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	{
		var errOut bytes.Buffer
		i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: "exec-string"}
		if AuthorizeLine(cfg, root, i, execLine, &errOut, nil) {
			t.Fatal("non-claude agent unexpectedly allowed claude-prefix pass-through")
		}
	}
	if err := a.Sync("claude"); err != nil {
		t.Fatal(err)
	}

	// Tampered stable `ikesu` symlink must NOT pass through.
	stableIkesu := filepath.Join(root, "guard", "shell", "p_22", "ikesu")
	if err := os.Remove(stableIkesu); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", stableIkesu); err != nil {
		t.Fatal(err)
	}
	{
		var errOut bytes.Buffer
		i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: "exec-string"}
		if AuthorizeLine(cfg, root, i, execLine, &errOut, nil) {
			t.Fatal("tampered stable ikesu symlink unexpectedly allowed")
		}
	}
	if err := os.Remove(stableIkesu); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("current", "ikesu"), stableIkesu); err != nil {
		t.Fatal(err)
	}

	// Wrong permissions (0755 instead of 0700) on claude-prefix must NOT pass through.
	if err := os.Chmod(prefixPath, 0755); err != nil {
		t.Fatal(err)
	}
	{
		var errOut bytes.Buffer
		i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: "exec-string"}
		if AuthorizeLine(cfg, root, i, execLine, &errOut, nil) {
			t.Fatal("world-readable claude-prefix unexpectedly allowed")
		}
	}
	if err := os.Chmod(prefixPath, 0700); err != nil {
		t.Fatal(err)
	}

	// Tampered claude-prefix script must NOT pass through.
	if err := os.WriteFile(prefixPath, []byte("#!/bin/sh\necho tampered\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	i := Input{Cwd: cwd, LaunchCwd: cwd, Env: EnvMap(env), LineVia: "exec-string"}
	if AuthorizeLine(cfg, root, i, execLine, &errOut, nil) {
		t.Fatal("tampered claude-prefix script unexpectedly allowed")
	}
}
