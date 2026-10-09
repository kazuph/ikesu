package tui

import (
	"github.com/kazuph/goterrace/internal/render"
	"path/filepath"
	"testing"
)

func TestG5TaskOSCAndBranchRemainSeparateAtRuntime(t *testing.T) {
	a := specRuntimeApp(t)
	cwd := a.st.Workspaces[0].Cwd
	sidebarGit(t, cwd, "init", "-qb", "feature")
	p, err := a.sup.SpawnWith(1, cwd, 90, 25, nil, []string{"/bin/sh", "-c", "printf '\033]0;thinking\007READY'; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	a.SetReported(1, "codex", "working")
	a.RecordSession(1, AgentRecord{Agent: "codex", Title: "restore pane sessions"})
	snap := a.paneSnaps()[1]
	if snap.Title != "thinking" || snap.TaskTitle != "restore pane sessions" || snap.Branch != "feature" || snap.Label != "codex" {
		t.Fatal(snap)
	}
	if name := render.WorkspaceDisplayName(a.st.Workspaces[0], a.paneSnaps()); name != filepath.Base(cwd)+"-restore pane sessions" {
		t.Fatal(name)
	}
	a.SetReported(1, "", "")
	a.ClearSession(1)
	if name := render.WorkspaceDisplayName(a.st.Workspaces[0], a.paneSnaps()); name != filepath.Base(cwd) {
		t.Fatal("shell OSC mixed into name", name)
	}
}
