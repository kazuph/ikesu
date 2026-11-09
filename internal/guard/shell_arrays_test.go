package guard

import (
	"strings"
	"testing"
)

func TestRuntimeDynamicNamesAndArrays(t *testing.T) {
	for _, tc := range []struct{ snapshot, line, decision string }{
		{`declare -a C=([0]="/bin/echo")`, `"${C[0]}" ok`, "allow"},
		{`declare -a C='([0]="/bin/echo")'`, `"${C[0]}" ok`, "allow"},
		{`declare -a C=([1000000000]="/bin/echo")`, `"${C[1000000000]}" ok`, "allow"},
		{"declare -- N=\"3\"\ndeclare -a C=([3]=\"/bin/echo\")", `"${C[$N]}" ok`, "allow"},
		{`declare -a C=([0]="/bin/rm")`, `"${C[0]}" /outside/victim`, "ask"},
		{`declare -- C="/bin/rm"`, `"$C" /outside/victim`, "ask"},
		{`declare -a C=([0]="/bin/echo")`, `"${C[8]}" ok`, "allow"}, // 未確定のコマンド名は通す（2026-11-10）
		{`declare -a C=([0]="/bin/echo")`, `"${C[$UNKNOWN]}" ok`, "allow"},
		{"", `$(printf /bin/rm) /outside/victim`, "allow"},
		{"", `env $(printf /bin/rm) /outside/victim`, "allow"},
		{"", ": > \"$(printf %s \"$IKESU_GUARD_MARKER\")\"", "ask"},
		{`declare -- TARGET="/outside/file"`, `: > "$TARGET"`, "allow"},
	} {
		t.Run(tc.line+tc.snapshot, func(t *testing.T) {
			i := WithShellVariables(Input{Cwd: t.TempDir(), Env: map[string]string{}}, strings.NewReader(tc.snapshot))
			selected, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.decision {
				t.Fatalf("%+v want %s", v, tc.decision)
			}
			if strings.Contains(tc.line, "${C[") && !strings.Contains(tc.line, "[8]") && !strings.Contains(tc.line, "UNKNOWN") && v.Decision == "allow" && !selected.probePrivate {
				// lineVerdict's aggregate input can remain the original for allow; inspect
				// the expanded command directly to prove probes see only original exports.
				commands, err := lineCommands(i, tc.line)
				if err != nil || len(commands) != 1 || !commands[0].probePrivate {
					t.Fatalf("array expansion did not protect probes: %+v %v", commands, err)
				}
			}
		})
	}
}
