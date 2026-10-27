package teststate

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

var originalGoEnvironment map[string]string

func readGoEnvironment() (map[string]string, error) {
	output, err := exec.Command("go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err != nil {
		return nil, err
	}
	var values map[string]string
	err = json.Unmarshal(output, &values)
	return values, err
}

func TestHomeIsolationPreservesGoToolCaches(t *testing.T) {
	after, err := readGoEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range originalGoEnvironment {
		t.Logf("%s before=%q after=%q isolated HOME=%q", key, want, after[key], os.Getenv("HOME"))
		if after[key] != want {
			t.Errorf("HOME isolation redirected %s into an empty cache: %q, want %q", key, after[key], want)
		}
	}
}
