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
