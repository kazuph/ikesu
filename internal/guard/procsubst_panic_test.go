package guard

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportedClaudeProcessSubstitutionDoesNotPanic(t *testing.T) {
	line := `cmp -l catppuccin-hover.ansi catppuccin-no-hover.ansi | wc -l; diff <(python3 -I -c "import json;d=json.load(open('catppuccin-hover-cells.json'));print(json.dumps(d)[:0])") /dev/null; python3 -I -c "
import json
a=json.load(open('catppuccin-hover-cells.json')); b=json.load(open('catppuccin-no-hover-cells.json'))
print(type(a).__name__, list(a)[:6] if isinstance(a,dict) else len(a))
"
`
	root := t.TempDir()
	a := NewAssets(root, "p_1", "/usr/bin/true", Config{})
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/no-server"}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Sync("claude"); err != nil {
		t.Fatal(err)
	}
	i := Input{Cwd: root, Env: EnvMap(env)}
	// Exact no-snapshot Claude bootstrap reported by the parent. Only the
	// authorization implementation runs: never Execute or an external shell.
	wrapped := `{ shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval ` + quote(line) + ` < /dev/null && pwd -P >| /tmp/claude-procsubst-panic-cwd`
	payload := ClaudePrefixLine(i, wrapped)
	if payload != line {
		t.Fatal("fixture did not exercise the reported Claude payload")
	}
	var output bytes.Buffer
	if AuthorizeLine(Config{}, root, i, payload, &output, nil) {
		t.Fatal("process substitution must require approval")
	}
	if !strings.Contains(output.String(), "プロセス置換") {
		t.Fatalf("missing process substitution refusal: %s", output.String())
	}
}
