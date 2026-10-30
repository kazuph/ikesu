package tui

import (
	"io"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestViewerUsesSessionBranchCacheAndReleasesWatches(t *testing.T) {
	teststate.Lock(t)
	a, repo := worktreeApp(t)
	t.Cleanup(func() { a.pruneBranchCache(nil) })
	viewer := a.NewViewer(io.Discard, 120, 35, nil)
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, repo, 80, 24, nil, []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	a.paneSnaps()
	old := a.branchCache[filepath.Clean(repo)]
	if old == nil {
		t.Fatal("session did not create cache")
	}
	viewer.with(a, func(ui *App) { ui.sidebarWheel(uv.Mouse{}, false) })
	if viewer.app.branchCache != nil {
		t.Fatal("viewer created its own branch cache")
	}
	later := a.NewViewer(io.Discard, 120, 35, nil)
	if later.app.branchCache != nil {
		t.Fatal("viewer copied an existing session cache")
	}
	next, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteToPTY([]byte("cd " + strconv.Quote(next) + "\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.PollProcess()
		if p.Snapshot().Cwd == next {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if p.Snapshot().Cwd != next {
		t.Fatal("pane cwd did not change")
	}
	a.paneSnaps()
	if a.branchCache[filepath.Clean(repo)] != nil || old.watch.Update([]string{repo}) {
		t.Fatal("old cwd watch survived movement")
	}
	current := a.branchCache[filepath.Clean(next)]
	if current == nil {
		t.Fatal("new cwd missing")
	}
	a.sup.Kill(pid)
	a.paneSnaps()
	viewer.Detach(a)
	if len(a.branchCache) != 0 || current.watch.Update([]string{next}) || viewer.app.branchCache != nil {
		t.Fatal("watch survived pane exit or viewer disconnect")
	}
}
