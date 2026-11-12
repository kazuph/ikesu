package guard

import (
	"os"
	"strings"
	"testing"
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
