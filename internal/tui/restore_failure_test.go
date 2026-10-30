package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRestoreShowsShellSpawnFailure(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	teststate.Setenv(t, "SHELL", dir+"/missing-shell")
	st := model.New(dir, 120, 35)
	sup := runtime.NewSupervisor()
	t.Cleanup(sup.KillAll)
	a := NewApp(st, sup, io.Discard, dir)
	other := filepath.Join(dir, "saved-cwd")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	saved := st.Snapshot()
	saved.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) { p.Cwd = other })
	a.RestoreSession(saved)
	a.snapshotSink = func(in render.Input) error {
		if in.ConfigWarningBanner || !strings.Contains(in.ConfigWarning, "p1") || !strings.Contains(in.ConfigWarning, "missing-shell") {
			t.Fatalf("spawn failure not visible: %q", in.ConfigWarning)
		}
		frame := render.Compose(in).Frame
		var screen strings.Builder
		for y := 0; y < frame.H; y++ {
			for x := 0; x < frame.W; x++ {
				if cell := frame.Cell(x, y); cell != nil {
					screen.WriteString(cell.Content)
				}
			}
		}
		if !strings.Contains(screen.String(), "pane p1: no such file or directory") {
			t.Fatalf("spawn failure missing from screen: %s", screen.String())
		}
		return nil
	}
	a.draw()
}
