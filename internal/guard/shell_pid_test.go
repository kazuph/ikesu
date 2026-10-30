package guard

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeShellPIDRedirect(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ name, pid, target, decision, reason string }{
		{"statusline", "123", `${RL_CACHE}.tmp.$$`, "allow", ""},
		{"missing", "", `${RL_CACHE}.tmp.$$`, "ask", "書き込み先のパスを安全に確定できません"},
		{"invalid", "12x", `${RL_CACHE}.tmp.$$`, "ask", "書き込み先のパスを安全に確定できません"},
		{"background", "123", `${RL_CACHE}.$!`, "ask", "書き込み先のパスを安全に確定できません"},
		{"status", "123", `${RL_CACHE}.$?`, "ask", "書き込み先のパスを安全に確定できません"},
		{"argument", "123", `${RL_CACHE}.$1`, "ask", "書き込み先のパスを安全に確定できません"},
		{"guard", "123", `${GUARD}/marker.$$`, "ask", "ガードの置き場所へのリダイレクトには承認が必要です"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := WithShellVariables(Input{Cwd: root, GuardRoot: filepath.Join(root, "guard"), Env: map[string]string{}}, strings.NewReader(fmt.Sprintf("declare -- RL_CACHE=%q\ndeclare -- GUARD=%q\n", filepath.Join(root, "cache"), filepath.Join(root, "guard"))))
			i = WithShellPID(i, tc.pid)
			_, v, _ := lineVerdict(Config{}, i, `jq -c '{rate_limits}' > "`+tc.target+`" 2> /dev/null`)
			if v.Decision != tc.decision || !strings.Contains(v.Reason, tc.reason) {
				t.Fatalf("got %+v", v)
			}
		})
	}
}
