package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExpandedEnvironmentSecretsUseVariableNamesInEverySurface(t *testing.T) {
	for _, key := range []string{"OPENAI_API_TOKEN", "api_key", "BUILD_SECRET", "PASSWORD", "DbPass", "ApiAuth", "CREDENTIAL", "CookieValue", "SessionValue"} {
		value := "unstructured-value-without-a-known-prefix"
		p, feedback, record := presentationFixture(t, "curl", []string{"-X", "POST", "https://example.test", "-H", "Authorization: Bearer " + value, "--data", value}, "", map[string]string{key: value})
		encoded, _ := json.Marshal(p)
		for surface, text := range map[string]string{"decision": string(encoded), "feedback": feedback, "record": record} {
			if strings.Contains(text, value) {
				t.Errorf("%s exposed expanded environment value for %s", surface, key)
			}
			if !strings.Contains(text, "$"+key) {
				t.Errorf("%s did not restore variable name for %s", surface, key)
			}
		}
	}
}

func TestEnvironmentSecretDisplayDoesNotChangeVerdictOrExecuteBytes(t *testing.T) {
	value := "opaque original bytes"
	input := Input{Command: "curl", Args: []string{"-X", "POST", "https://example.test", "--data", value}, Env: map[string]string{"API_TOKEN": value}}
	before, _ := json.Marshal(input)
	verdict := Evaluate(Config{}, input)
	input.redact(input.Line())
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("display changed the original input")
	}
	if got := Evaluate(Config{}, input); got.Decision != verdict.Decision || got.Rule != verdict.Rule {
		t.Fatal("display changed policy")
	}
}

func TestEnvironmentSecretNameAndLengthBoundaries(t *testing.T) {
	cases := []struct{ name, value, want string }{
		{"PUBLIC_VALUE", "abcdefgh", "abcdefgh"},
		{"TOKEN", "abcdefg", "abcdefg"},
		{"TOKEN", "abcdefgh", "$TOKEN"},
		{"AUTH", "あいうえおかき", "あいうえおかき"},
		{"AUTH", "あいうえおかきく", "$AUTH"},
	}
	for _, tc := range cases {
		i := Input{Env: map[string]string{tc.name: tc.value}}
		if got := i.redact(tc.value); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
	i := Input{Env: map[string]string{"LONG_TOKEN": "abcdefghijkl", "SHORT_TOKEN": "abcdefgh", "ALIAS_KEY": "abcdefghijkl"}}
	if got := i.redact("abcdefghijkl abcdefgh Authorization: Bearer abcdefgh TOKEN=abcdefgh sk-project-TESTONLY"); got != "$ALIAS_KEY $SHORT_TOKEN Authorization: Bearer $SHORT_TOKEN TOKEN=$SHORT_TOKEN ***" {
		t.Fatal("overlapping values or legacy masking changed:", got)
	}
}
