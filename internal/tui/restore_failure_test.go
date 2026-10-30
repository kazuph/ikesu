package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRestoreShowsShellSpawnFailure(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	teststate.Setenv(t, "SHELL", dir+"/missing-shell")
	st := model.New(dir, 240, 35)
	sup := runtime.NewSupervisor()
	t.Cleanup(sup.KillAll)
	a := NewApp(st, sup, io.Discard, dir)
	a.RestoreSession(st.Snapshot())
	a.snapshotSink = func(in render.Input) error {
		if !in.ConfigWarningBanner || !strings.Contains(in.ConfigWarning, "p1") || !strings.Contains(in.ConfigWarning, "missing-shell") {
			t.Fatalf("spawn failure not visible: %q", in.ConfigWarning)
		}
		return nil
	}
	a.draw()
}
