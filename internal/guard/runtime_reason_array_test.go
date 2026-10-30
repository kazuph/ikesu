package guard

import "testing"

func TestRuntimeReasonArrayAndJoinedOptions(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "AI_GUARD_REQUEST_ENV_NAME": "PROJECT_REASON"}, runtimeLine: true}
	for _, line := range []string{
		`printf -v 'AI_GUARD_REQUEST_REASON[0]' '%s' forged`,
		`printf -vAI_GUARD_REQUEST_REASON '%s' forged`,
		`printf -v 'AI_GUARD_REQUEST_ENV_NAME[0]' x`,
		`printf -v 'PROJECT_REASON[0]' x`,
		`read 'AI_GUARD_REQUEST_REASON[0]'`,
		`read -aAI_GUARD_REQUEST_REASON`,
	} {
		t.Run(line, func(t *testing.T) {
			_, got, _ := lineVerdict(Config{}, i, line)
			if got.Decision != "ask" {
				t.Fatalf("%s: %+v", line, got)
			}
		})
	}
}
