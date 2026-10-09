package tui

import (
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestG9NewWorkspaceUsesCachedPaneCwdAfterSessionRender(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(a.focusedPane())
	next := t.TempDir()
	next, _ = filepath.EvalSymlinks(next)
	if _, err := p.WriteToPTY([]byte("cd '" + next + "'; printf '%s%s\\n' CACHED_CWD_ READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "CACHED_CWD_READY")
	p.PollProcess()
	if p.Snapshot().Cwd != next {
		t.Fatal(p.Snapshot().Cwd, next)
	}
	a.draw()
	other := t.TempDir()
	other, _ = filepath.EvalSymlinks(other)
	if _, err := p.WriteToPTY([]byte("cd '" + other + "'; printf '%s%s\\n' NEXT_CWD_ READY\n")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "NEXT_CWD_READY")
	p.PollProcess()
	if p.Snapshot().Cwd != other {
		t.Fatal("runtime cwd not current")
	}
	a.prefixKey(uv.Key{Code: 'N'})
	if len(a.st.Workspaces) != 2 || a.st.ActiveWorkspace().Cwd != next {
		t.Fatal("workspace cwd did not come from cached pane state", a.st.ActiveWorkspace())
	}
}
