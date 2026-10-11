package runtime

import (
	"github.com/kazuph/goterrace/internal/guard"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardRuntimeMarkerLifecycle(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	teststate.Setenv(t, "HOME", d)
	teststate.Setenv(t, "ZDOTDIR", d)
	teststate.Setenv(t, "BASH_ENV", "")
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := NewSupervisor()
	if e := s.WaitLoginPath(); e != nil {
		t.Fatal(e)
	}
	s.SetGuard(guard.Config{Mode: "record"}, d)
	t.Cleanup(s.KillAll)
	marker := func(id string) string { return filepath.Join(d, "guard", "agent-panes", id+".json") }
	exists := func(p string) bool { _, e := os.Stat(p); return e == nil }
	p, e := s.SpawnWith(1, d, 80, 24, nil, []string{"/bin/sleep", "60"})
	if e != nil {
		t.Fatal(e)
	}
	if exists(marker("p_1")) {
		t.Fatal("human pane marked")
	}
	p.SetAgent("codex")
	if !exists(marker("p_1")) {
		t.Fatal("agent metadata not synchronized")
	}
	p.SetAgent("")
	p.PollProcess()
	if exists(marker("p_1")) {
		t.Fatal("clear resurrected marker")
	}
	p.SetAgent("codex")
	s.Kill(1)
	if exists(marker("p_1")) {
		t.Fatal("close left marker")
	}
	p, e = s.SpawnAgent(2, d, 80, 24, nil, []string{"/bin/sleep", "60"})
	if e != nil {
		t.Fatal(e)
	}
	if !exists(marker("p_2")) {
		t.Fatal("agent start not marked")
	}
	p.SetAgent("")
	p.PollProcess()
	if exists(marker("p_2")) {
		t.Fatal("explicit clear re-inherited start marker")
	}
	s.Kill(2)
	p, e = s.SpawnAgent(3, d, 80, 24, nil, []string{"/bin/sh", "-c", "exit 0"})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit")
	}
	if exists(marker("p_3")) {
		t.Fatal("exit left marker")
	}
	p, e = s.SpawnPopup(4, d, 80, 24, nil, []string{"/bin/sleep", "60"})
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgent("codex")
	if p.guard != nil || exists(marker("p_4")) {
		t.Fatal("identityless popup injected")
	}
	s.Kill(4)
}
