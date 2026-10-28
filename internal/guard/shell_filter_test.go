package guard

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the generated builtin-only selector without installing any trap.
func TestBashCheapSelector(t *testing.T) {
	for name, shell := range map[string]string{"system": "/bin/bash", "homebrew": "/opt/homebrew/bin/bash"} {
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				if name == "homebrew" && os.IsNotExist(err) {
					t.Skip("Homebrew bashがないため、この版だけ未実施")
				}
				t.Fatal(err)
			}
			a := NewAssets(t.TempDir(), "selector", "/unused", Config{Targets: []string{"custom-target", "rm"}})
			hook := a.debugHook("bash")
			start := strings.Index(hook, "_ikesu_guard_needs_check() {")
			end := strings.Index(hook, "_ikesu_guard_debug() {")
			selector := bashSimpleForms + hook[start:end]
			for _, tc := range []struct {
				line  string
				check bool
			}{
				{"printf '%s' hi", false}, {" /usr/bin/printf hi", false}, {"jq -r .model", false}, {"custom-target read", true}, {"/bin/rm /outside", true},
				{"printf x > out", true}, {"printf x >> out", true}, {"printf x >| out", true}, {"printf x &> out", true},
				{"\"rm\" /outside", true}, {"$CMD /outside", true}, {"$(printf rm) /outside", true}, {"r\\m /outside", true}, {"T=x /bin/rm /outside", true},
				{"X=1", false}, {`X="a > b"`, false}, {"X=1 Y=2", false}, {"local v=1 width=2", false}, {"declare X=1", false}, {"declare -- X=1", false}, {"[[ $i -eq 0 ]]", false}, {`[[ -z "$TERM_WIDTH" || "$TERM_WIDTH" -le 0 ]]`, false}, {"(( i++ ))", false}, {"(( i < 12 ))", false}, {"[[ $v == x ]]", false}, {"[[ x > y ]]", false},
				{"X=1 > out", true}, {"[[ x == y ]] > out", true}, {"(( i++ )) > out", true}, {"local -n X=PATH", true}, {"declare -i X=1", true}, {"PATH=x", true}, {"local AI_GUARD_DANGER_WORD_ACK=1", true}, {"declare IKESU_PANE_ID=other", true}, {"BASH_ENV=x", true}, {"ENV=x", true}, {"CLAUDE_CODE_SHELL_PREFIX=x", true}, {"(( PATH=1 ))", true},
				{"eval ':'", true}, {"source path", true}, {". path", true}, {"xargs rm", true}, {"find . -exec rm {} \\;", true}, {"bash -c ':'", true}, {"command rm", true}, {"builtin eval ':'", true}, {"exec rm", true},
				{"unset _ikesu_guard_word", true}, {"declare _ikesu_guard_word=x", true}, {"set +T", true}, {"trap ':' DEBUG", true}, {"_ikesu_guard_needs_check() { :; }", true},
			} {
				for _, ifs := range []string{":", ""} {
					script := selector + "\nIFS=" + quote(ifs) + "\n_ikesu_guard_needs_check " + quote(tc.line) + "\n"
					ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
					cmd := exec.CommandContext(ctx, shell, "--noprofile", "--norc", "-c", script)
					cmd.Env = []string{"PATH=/usr/bin:/bin"}
					out, err := cmd.CombinedOutput()
					timedOut := ctx.Err() != nil
					cancel()
					if timedOut || (err == nil) != tc.check {
						t.Fatalf("line=%q IFS=%q wantCheck=%v err=%v out=%s", tc.line, ifs, tc.check, err, out)
					}
				}
			}
		})
	}
}
