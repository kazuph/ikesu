package cli

import (
	"strings"
	"testing"
)

func TestG9PromptFlagsRequireWaitAndValidateStatus(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{[]string{"agent", "prompt", "p1", "text", "--until", "idle"}, "--until requires --wait"},
		{[]string{"agent", "prompt", "p1", "text", "--timeout", "0"}, "--timeout requires --wait"},
		{[]string{"agent", "prompt", "p1", "text", "--wait", "--until", "starting"}, "invalid --until status"},
		{[]string{"agent", "prompt", "p1", "text", "--wait", "--timeout", "-1"}, "invalid --timeout value"},
	} {
		code, _, errOut := runCLI(t, Options{}, tc.args...)
		if code != 2 || !strings.Contains(errOut, tc.message) {
			t.Fatal(tc, code, errOut)
		}
	}
}
