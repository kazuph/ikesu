package tui

import (
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestNewTabAndSplitFollowSelectedPaneDirectory(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "tab", true: "split"}[split], func(t *testing.T) {
			teststate.Lock(t)
			a := specRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			p := a.sup.Get(a.focusedPane())
			next, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.WriteToPTY([]byte("cd '" + next + "'; printf '%s%s\\n' FOLLOW_ READY\n")); err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "FOLLOW_READY")
			p.PollProcess()
			if p.Snapshot().Cwd != next {
				t.Fatal("source pane directory", p.Snapshot().Cwd, next)
			}
			if split {
				a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
			} else {
				a.exec(model.CmdNewTab{Workspace: a.st.Active})
			}
			created := a.sup.Get(a.focusedPane())
			if created == p || created == nil || created.SpawnCwd() != next {
				t.Fatalf("new pane directory = %v, want %s", created.Snapshot().Cwd, next)
			}
		})
	}
}
