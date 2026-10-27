package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestQuotedEnvironmentValueRestoresRawShellRepresentation(t *testing.T) {
	for _, fixture := range []struct{ value, quoted string }{
		{"RAW_SECRET_HEAD'RAW_SECRET_TAIL", `'RAW_SECRET_HEAD'\''RAW_SECRET_TAIL'`},
		{"RAW_SECRET_HEAD'RAW_SECRET_TAIL", `"RAW_SECRET_HEAD'"'RAW_SECRET_TAIL'`},
		{"RAW_SECRET_HEAD\nRAW_SECRET_TAIL", "'RAW_SECRET_HEAD'\"\n\"'RAW_SECRET_TAIL'"},
		{"RAW_SECRET_HEAD'\nRAW_SECRET_TAIL", "'RAW_SECRET_HEAD'\\''\nRAW_SECRET_TAIL'"},
	} {
		raw := "curl -X POST https://example.test --data " + fixture.quoted
		actual, err := exec.Command("/bin/sh", "-c", `printf '%s\0' `+raw).Output()
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Split(strings.TrimSuffix(string(actual), "\x00"), "\x00")
		want := []string{"curl", "-X", "POST", "https://example.test", "--data", fixture.value}
		if !reflect.DeepEqual(argv, want) {
			t.Fatalf("shell fixture differs: %q", argv)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestSecretExecutionReceivesOriginalHelper$")
		child.Env = append(os.Environ(), "GUARD_SECRET_EXEC_HELPER=1", "GUARD_SECRET_EXEC_VALUE="+fixture.value, "API_TOKEN="+fixture.value)
		received, err := child.Output()
		if err != nil || !bytes.Equal(received, []byte("PASSWORD="+fixture.value)) {
			t.Fatal("guard execution changed original bytes", err)
		}
		parsed, err := LineInput(Input{Env: map[string]string{"API_TOKEN": fixture.value}}, raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, displayArgv := range [][]string{argv, append([]string{parsed.Command}, parsed.Args...)} {
			p, feedback, record := presentationFixture(t, displayArgv[0], displayArgv[1:], raw, map[string]string{"API_TOKEN": fixture.value})
			if p.Body == nil || strings.Contains(*p.Body, "対象コマンド：") {
				t.Error("equivalent shell quoting repeated the command")
			}
			encoded, _ := json.Marshal(p)
			for surface, text := range map[string]string{"decision": string(encoded), "feedback": feedback, "record": record} {
				if strings.Contains(text, "RAW_SECRET_HEAD") || strings.Contains(text, "RAW_SECRET_TAIL") {
					t.Errorf("%s retained reconstructable environment secret for %q: %s", surface, fixture.quoted, text)
				}
				if !strings.Contains(text, "$API_TOKEN") {
					t.Errorf("%s did not retain restored variable", surface)
				}
			}
		}
	}
}
