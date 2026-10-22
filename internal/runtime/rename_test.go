package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStageOnePaneEnvironment(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "IKESU_PANE_ID", "p99")
	teststate.Setenv(t, "IKESU_SOCKET_PATH", "/inherited/ikesu.sock")
	teststate.Setenv(t, "IKESU_SOCKET_PATH", "/inherited/ikesu.sock")
	teststate.Setenv(t, "IKESU_GUARD_MARKER", "/inherited/guard")
	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	sup := NewSupervisor()
	defer sup.KillAll()
	sup.Env = func(int) []string {
		return []string{"IKESU_PANE_ID=p7", "IKESU_SOCKET_PATH=/fresh/ikesu.sock", "IKESU_SOCKET_PATH_EXPLICIT=1"}
	}
	pane, err := sup.SpawnWith(7, dir, 80, 24, nil, []string{"/bin/sh", "-c", "/usr/bin/env > " + path})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-pane.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			env[key] = value
		}
	}
	for key, want := range map[string]string{"IKESU": "1", "GOTERRACE": "1", "IKESU_PANE_ID": "p7", "IKESU_SOCKET_PATH": "/fresh/ikesu.sock"} {
		if env[key] != want {
			t.Errorf("%s=%q want %q", key, env[key], want)
		}
	}
	if env["IKESU_GUARD_MARKER"] != "" {
		t.Fatal("parent guard leaked")
	}
}
