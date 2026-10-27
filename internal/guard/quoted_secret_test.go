package guard

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestSecretArgvBoundariesSurviveRealShell(t *testing.T) {
	for _, value := range []string{"SECRET_HEAD_TESTONLY SECRET_TAIL_TESTONLY", "SECRET_HEAD_TESTONLY\tSECRET_TAIL_TESTONLY", "SECRET_HEAD_TESTONLY\nSECRET_TAIL_TESTONLY", "SECRET_HEAD_TESTONLY\"' SECRET_TAIL_TESTONLY"} {
		argv := []string{"curl", "-X", "POST", "https://example.test", "-F", "PASSWORD=" + value}
		var quoted []string
		for _, a := range argv {
			q, err := syntax.Quote(a, syntax.LangBash)
			if err != nil {
				t.Fatal(err)
			}
			quoted = append(quoted, q)
		}
		raw := strings.Join(quoted, " ")
		output, err := exec.Command("/bin/sh", "-c", `printf '%s\0' `+raw).Output()
		if err != nil {
			t.Fatal(err)
		}
		actual := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
		if !reflect.DeepEqual(actual, argv) {
			t.Fatal("real shell changed fixture bytes")
		}
		for _, line := range []string{"", raw} {
			decision, feedback, record := presentationFixture(t, actual[0], actual[1:], line, nil)
			payload := fmtDecisionForSecretTest(decision.Title, decision.Body, decision.Origin.Command)
			for name, text := range map[string]string{"decision and connector payload": payload, "feedback": feedback, "record": record} {
				if strings.Contains(text, "SECRET_HEAD_TESTONLY") || strings.Contains(text, "SECRET_TAIL_TESTONLY") {
					t.Errorf("%s leaked part of a real argv secret (raw=%v)", name, line != "")
				}
			}
		}
		child := exec.Command(os.Args[0], "-test.run=^TestSecretExecutionReceivesOriginalHelper$")
		child.Env = append(os.Environ(), "GUARD_SECRET_EXEC_HELPER=1", "GUARD_SECRET_EXEC_VALUE="+value)
		received, err := child.Output()
		if err != nil {
			t.Fatal("real execute destination failed", err)
		}
		if !bytes.Equal(received, []byte("PASSWORD="+value)) {
			t.Fatal("redaction changed executed argv bytes")
		}
	}
}
func fmtDecisionForSecretTest(title string, body *string, command string) string {
	if body != nil {
		return title + "\n" + *body + "\n" + command
	}
	return title + command
}
func TestSecretExecutionReceivesOriginalHelper(t *testing.T) {
	if os.Getenv("GUARD_SECRET_EXEC_HELPER") != "1" {
		return
	}
	i := Input{Command: "/usr/bin/printf", Args: []string{"%s", "PASSWORD=" + os.Getenv("GUARD_SECRET_EXEC_VALUE")}, Cwd: t.TempDir(), Env: EnvMap(os.Environ())}
	if err := Log(t.TempDir(), i, Marker{}, result("allow", "test", "実行先のバイト列を確認"), "test"); err != nil {
		t.Fatal(err)
	}
	if err := Execute(i); err != nil {
		t.Fatal(err)
	}
}
