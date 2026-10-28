package teststate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func TestIsolatedGoTelemetryOffDoesNotCreateLateFiles(t *testing.T) {
	output, err := exec.Command("go", "env", "-json", "GOTELEMETRY", "GOTELEMETRYDIR").Output()
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]string
	if err = json.Unmarshal(output, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["GOTELEMETRY"] != "off" {
		t.Fatal("isolated Go telemetry remains enabled", settings)
	}
	local := filepath.Join(settings["GOTELEMETRYDIR"], "local")
	files := func() []string {
		entries, err := os.ReadDir(local)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	before := files()
	for i := 0; i < 10; i++ {
		// Keep the isolated HOME: every query exercises the actual test environment.
		if _, err := readGoEnvironment(); err != nil {
			t.Fatal(err)
		}
		if got := files(); !reflect.DeepEqual(before, got) {
			t.Fatal("go env created telemetry files after off", before, got)
		}
	}
	t.Logf("telemetry=off isolated HOME=%s directory=%s go env calls=10 local files unchanged=%v", os.Getenv("HOME"), local, before)
}
