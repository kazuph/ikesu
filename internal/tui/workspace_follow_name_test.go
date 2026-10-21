package tui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestAutomaticWorkspaceNameFollowsFirstShellAndPreservesCustomName(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(a.focusedPane())
	refresh := func() {
		t.Helper()
		a.gitAt = time.Time{}
		a.refreshWorkspaceGit(time.Now())
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-time.After(5 * time.Second):
			t.Fatal("workspace metadata did not finish")
		}
	}
	for i, custom := range []bool{false, true} {
		dir := t.TempDir()
		if custom {
			a.exec(model.CmdRenameWorkspace{Index: 0, Name: "chosen"})
		}
		marker := fmt.Sprintf("NAME_%d_READY", i)
		if _, err := p.WriteToPTY([]byte(fmt.Sprintf("cd '%s'; printf 'NAME_%%s_READY\\n' %d\n", dir, i))); err != nil {
			t.Fatal(err)
		}
		specWaitText(t, p, marker)
		p.PollProcess()
		refresh()
		want := filepath.Base(dir)
		if custom {
			want = "chosen"
		}
		if a.workspaceNames[0] != want || a.WorkspaceLabel(0) != want {
			t.Fatalf("sidebar=%q API label=%q, want %q", a.workspaceNames[0], a.WorkspaceLabel(0), want)
		}
	}
}
