package runtime

import (
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPaneChildSessionScrubAndExplicitExtras(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "CLAUDE_CODE_CHILD_SESSION", "1")
	teststate.Setenv(t, "CODEX_THREAD_ID", "inherited")
	teststate.Setenv(t, "IKESU_GUARD_ENFORCING", "1")
	teststate.Setenv(t, "CLAUDE_EFFORT", "high")
	dir := t.TempDir()
	sup := NewSupervisor()
	defer sup.KillAll()
	for i := 1; i <= 2; i++ {
		path := filepath.Join(dir, itoa(i))
		extras := []string{}
		if i == 2 {
			extras = append(extras, "CODEX_THREAD_ID=explicit")
		}
		pane, err := sup.SpawnWith(i, dir, 80, 24, extras, []string{"/bin/sh", "-c", "/usr/bin/env > " + path})
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
		m := guard.EnvMap(strings.Split(string(raw), "\n"))
		if m["CLAUDE_CODE_CHILD_SESSION"] != "" || m["IKESU_GUARD_ENFORCING"] != "" || m["CLAUDE_EFFORT"] != "high" {
			t.Fatal(m)
		}
		expected := ""
		if i == 2 {
			expected = "explicit"
		}
		if m["CODEX_THREAD_ID"] != expected {
			t.Fatal(m)
		}
	}
}
