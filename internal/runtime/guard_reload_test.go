package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/guard"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestGuardReloadWarnsUninjectedPaneAndClearsOnExit(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	teststate.Setenv(t, "HOME", d)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := NewSupervisor()
	t.Cleanup(s.KillAll)
	off := false
	s.SetGuard(guard.Config{Enabled: &off}, d)
	p, e := s.SpawnWith(1, d, 80, 24, nil, []string{"/bin/sleep", "60"})
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgent("claude")
	s.SetGuard(guard.Config{}, d)
	if !strings.Contains(s.GuardWarnings(), "p1") || !strings.Contains(s.GuardWarnings(), "起動時") {
		t.Fatal(s.GuardWarnings())
	}
	issues, e := guard.CoverageIssues(d)
	if e != nil || len(issues) != 1 {
		t.Fatal(issues, e)
	}
	if _, e = os.Stat(filepath.Join(d, "guard", "agent-panes", "p_1.json")); !os.IsNotExist(e) {
		t.Fatal("false marker", e)
	}
	p.SetAgent("")
	if s.GuardWarnings() != "" {
		t.Fatal("human warning", s.GuardWarnings())
	}
	p.SetAgent("claude")
	s.Kill(1)
	issues, e = guard.CoverageIssues(d)
	if e != nil || len(issues) != 0 {
		t.Fatal("closed warning", issues, e)
	}
}
