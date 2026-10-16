package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestHiddenViewerRequeriesMouseWhenShown(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[capture], func(t *testing.T) {
			st := model.New(t.TempDir(), 120, 40)
			app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
			var output bytes.Buffer
			viewer := app.NewViewer(&output, 120, 40, nil)
			viewer.app.sidebarConfig.UI.MouseCapture = &capture
			viewer.Begin(app, 0, 0)
			viewer.SetVisible(app, false)
			output.Reset() // Background endpoint output never reached the host terminal.
			viewer.SetVisible(app, true)
			query := "\x1b[?1016$p\x1b[16t\x1b[14t"
			if strings.Contains(output.String(), query) != capture {
				t.Fatal("visibility did not respect mouse negotiation settings", output.String())
			}
			if viewer.app.hostPixelMouse {
				t.Fatal("visibility fabricated terminal support")
			}
			viewer.Input(app, []byte("\x1b[?1016;2$y\x1b[6;16;8t"))
			if viewer.app.hostPixelMouse != capture || viewer.app.cellW != 8 || viewer.app.cellH != 16 {
				t.Fatal("real terminal reports did not negotiate pixel input")
			}
			viewer.SetVisible(app, true)
			if capture && strings.Count(output.String(), query) != 1 {
				t.Fatal("already visible endpoint repeated negotiation")
			}
			if app.hostPixelMouse || app.cellW != 0 || app.cellH != 0 {
				t.Fatal("viewer negotiation changed the session host state")
			}
		})
	}
}

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
