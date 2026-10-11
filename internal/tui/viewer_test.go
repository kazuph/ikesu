package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerSelectionAndDimensionsIndependent(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	st.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
	st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	original := app.selection()
	first := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	first.with(app, func(ui *App) { ui.st.Apply(model.CmdSelectTab{Workspace: 0, Tab: 1}) })
	second.with(app, func(ui *App) { ui.st.Apply(model.CmdSelectWorkspace{Index: 1}) })
	first.with(app, func(ui *App) {
		if ui.st.Active != 0 || ui.st.Workspaces[0].ActiveTab != 1 || ui.st.Cols != 80 || ui.st.Rows != 24 {
			t.Fatal("first view changed", ui.st)
		}
	})
	second.with(app, func(ui *App) {
		if ui.st.Active != 1 || ui.st.Workspaces[0].ActiveTab != 0 || ui.st.Cols != 160 || ui.st.Rows != 45 {
			t.Fatal("second view changed", ui.st)
		}
	})
	after := app.selection()
	if original.workspace != after.workspace || original.cols != after.cols || original.rows != after.rows {
		t.Fatal("navigation mutated session selection")
	}
	for id, tab := range original.tabs {
		if after.tabs[id] != tab {
			t.Fatal("navigation mutated session tab")
		}
	}
}

func TestViewerFramingDoesNotMixPartialInputs(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	first := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	first.app.fr.Push([]byte("\x1b["))
	if !first.app.fr.Pending() || second.app.fr.Pending() {
		t.Fatal("shared input framing")
	}
	second.app.fr.Push([]byte("x"))
	if !first.app.fr.Pending() {
		t.Fatal("second viewer flushed first")
	}
	first.app.windowBlurred = true
	if first.VisiblePane(app, 1) || !second.VisiblePane(app, 1) {
		t.Fatal("focus visibility not viewer-local")
	}
}

func TestViewersShareOneSessionMetadataRead(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	supervisor := runtime.NewSupervisor()
	defer supervisor.KillAll()
	app := NewApp(st, supervisor, &bytes.Buffer{}, t.TempDir())
	if err := app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	reads := 0
	app.SetPaneMetadataSource(func(int) api.PaneMetadataView { reads++; return api.PaneMetadataView{} })
	first := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	app.drawViewers = func() {}
	panes := app.SessionPaneSnaps()
	first.Draw(app, panes)
	second.Draw(app, panes)
	if reads != 1 {
		t.Fatalf("session metadata reads=%d for two viewers, want 1", reads)
	}
}
