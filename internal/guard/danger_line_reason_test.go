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
