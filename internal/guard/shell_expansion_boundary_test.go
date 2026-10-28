package guard

import (
	"strings"
	"testing"
)

func TestRuntimeReservedExpansionWritesAsk(t *testing.T) {
	for _, line := range []string{
		`X="$((BASH_ENV=0))"`, `declare X="${_ikesu_guard_debug_busy:=1}"`, `X="${_ikesu_guard_debug_busy=1}"`,
		`X="$((PATH++))"`, `X="$((--IKESU_GUARD_ENFORCING))"`, `X="$((AI_GUARD_DANGER_WORD_ACK+=1))"`,
		`X="${AI_GUARD_DANGER_WORD_ACK:=1}"`, `X="${A[BASH_ENV=0]}"`,
	} {
		t.Run(line, func(t *testing.T) {
			input := WithShellVariables(Input{Cwd: t.TempDir(), Env: map[string]string{}}, strings.NewReader(""))
			_, verdict, _ := lineVerdict(Config{}, input, line)
			if verdict.Decision != "ask" {
				t.Fatalf("reserved expansion allowed: %+v", verdict)
			}
		})
	}
	for _, line := range []string{`X="ordinary string"`, `X='${PATH:=bad}'`, `X="${V:-0}"`, `PCT=$(printf 1)`} {
		input := WithShellVariables(Input{Cwd: t.TempDir(), Env: map[string]string{}}, strings.NewReader(""))
		_, verdict, _ := lineVerdict(Config{}, input, line)
		if verdict.Decision != "allow" {
			t.Fatalf("safe expansion rejected %q: %+v", line, verdict)
		}
	}
}
