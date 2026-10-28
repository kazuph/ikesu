package guard

import "testing"

func TestRuntimeReasonMutationsRequireApproval(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "AI_GUARD_REQUEST_ENV_NAME": "PROJECT_REASON"}, runtimeLine: true}
	for _, line := range []string{
		`export -p AI_GUARD_REQUEST_REASON=forged`, `declare -p AI_GUARD_REQUEST_REASON=forged`, `printf "$(printf %s -v)" "$(printf %s ordinary)" %s forged`,
		`printf -v AI_GUARD_REQUEST_REASON %s forged`, `read AI_GUARD_REQUEST_REASON`, `let 'AI_GUARD_REQUEST_REASON=0'`,
		`export 'AI_GUARD_REQUEST_REASON=forged'`, `declare -x 'PROJECT_REASON=forged'`, `declare -n ref=other`,
		`printf -v "$TARGET" x`, `read "$TARGET"`, `readarray "$TARGET"`, `mapfile "$TARGET"`, `let "$TARGET=1"`,
		`declare -x "$TARGET=forged"`, `export "$TARGET=forged"`,
		`AI_GUARD_REQUEST_REASON='偽の申請文'`, `export IKESU_GUARD_PARENT_REASON=forged`, `unset AI_GUARD_REQUEST_REASON`,
		`AI_GUARD_REQUEST_REASON=forged /bin/bash child.sh`, `env AI_GUARD_REQUEST_REASON=forged /bin/bash child.sh`,
		`env -u PROJECT_REASON /bin/bash child.sh`, `env -i /bin/bash child.sh`, `PROJECT_REASON=forged`, `declare PROJECT_REASON=forged`,
		`command -- export PROJECT_REASON=forged`, `builtin export PROJECT_REASON=forged`, `export PROJECT_REASON`, `unset PROJECT_REASON`, `AI_GUARD_REQUEST_ENV_NAME=OTHER`, `X="${PROJECT_REASON:=forged}"`,
	} {
		_, v, _ := lineVerdict(Config{}, i, line)
		if v.Decision != "ask" && v.Decision != "block" {
			t.Errorf("line=%s verdict=%+v", line, v)
		}
	}
}

func TestRuntimeReasonDisplayOnly(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "AI_GUARD_REQUEST_ENV_NAME": "PROJECT_REASON"}, runtimeLine: true}
	for _, line := range []string{`declare -p AI_GUARD_REQUEST_REASON`, `declare -p PROJECT_REASON`, `export -p IKESU_GUARD_PARENT_REASON`, `printf '%s' hello`, `read -r ordinary`, `printf -v ordinary %s hello`} {
		_, v, _ := lineVerdict(Config{}, i, line)
		if v.Decision != "allow" {
			t.Errorf("display line=%s verdict=%+v", line, v)
		}
	}
}
