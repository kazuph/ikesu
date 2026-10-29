package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"io"
	"testing"
)

func TestPluginZoomedNoFocusKeepsCurrentPane(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	sup := runtime.NewSupervisor()
	defer sup.KillAll()
	app := NewApp(st, sup, io.Discard, "")
	before := st.FocusedPane()
	pid, err := app.OpenPluginPane(0, before, "zoomed", "right", t.TempDir(), []string{"/bin/cat"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if pid == before || st.FocusedPane() != before || st.ActiveWorkspace().TabRef().Zoomed != 0 {
		t.Fatalf("no-focus moved view: pane=%d zoom=%d", st.FocusedPane(), st.ActiveWorkspace().TabRef().Zoomed)
	}
}
