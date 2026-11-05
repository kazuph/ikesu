package guard

import (
	"strings"
	"testing"
)

func TestUnsupportedParamExpansionRequiresApproval(t *testing.T) {
	for _, op := range []string{"P", "A", "a"} {
		t.Run(op, func(t *testing.T) {
			line := `bash -c 'echo "${HOME@` + op + `}"'`
			i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": "/tmp/home", "PATH": "/usr/bin:/bin"}}
			_, v, err := lineVerdict(Config{}, i, line)
			if err == nil || v.Decision != "ask" || v.Rule != "shell-line" || !strings.Contains(v.Reason, "展開") {
				t.Fatalf("verdict=%+v err=%v", v, err)
			}
		})
	}
}
