package guard

import "testing"

func TestRuntimeReasonMutationsRequireApproval(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "AI_GUARD_REQUEST_ENV_NAME": "PROJECT_REASON"}, runtimeLine: true}
	for _, line := range []string{
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
