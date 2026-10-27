package guard

import (
	"encoding/json"
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
		p, feedback, record := presentationFixture(t, argv[0], argv[1:], raw, map[string]string{"API_TOKEN": fixture.value})
		encoded, _ := json.Marshal(p)
		for surface, text := range map[string]string{"decision": string(encoded), "feedback": feedback, "record": record} {
			if strings.Contains(text, "RAW_SECRET_HEAD") || strings.Contains(text, "RAW_SECRET_TAIL") {
				t.Errorf("%s retained reconstructable environment secret", surface)
			}
			if !strings.Contains(text, "$API_TOKEN") {
				t.Errorf("%s did not retain restored variable", surface)
			}
		}
	}
}
