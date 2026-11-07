package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestOyagoiViewerReusedNumberIsNotNavigatorHQ(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	ws := state.Workspaces[0]
	app := NewApp(state, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	viewer.SetOyagoi(app, ws.ID, ws.Generation)
	ws.Generation = "replacement-generation"
	viewer.app.navigator = &navigatorView{}
	rows, _ := viewer.app.navigatorRows()
	for _, row := range rows {
		if strings.Contains(row.Name, "oyagoi") {
			t.Fatal("replacement is displayed as HQ")
		}
	}
}

func TestOyagoiWorkspaceMenuUsesIdentityWhenSectionCollapsed(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	ws := state.Workspaces[0]
	ws.Section = model.SectionFavorite
	state.CollapsedSections = map[model.Section]bool{model.SectionFavorite: true}
	app := NewApp(state, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	app.SetServerUI()
	viewer := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	if !viewer.OpenWorkspaceMenu(app, ws.ID, ws.Generation, 3, 5) {
		t.Fatal("workspace menu not opened")
	}
	if viewer.app.menu == nil || viewer.app.menu.ws != 0 {
		t.Fatal("wrong menu target")
	}
	viewer.app.menu = nil
	if viewer.OpenWorkspaceMenu(app, ws.ID, "other-generation", 3, 5) || viewer.app.menu != nil {
		t.Fatal("stale identity opened menu")
	}
}

func TestOyagoiViewerProjectionIsPrivateAndChecksIdentity(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	ws := state.Workspaces[0]
	app := NewApp(state, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	first := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	second := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	first.SetOyagoi(app, ws.ID, ws.Generation)
	if first.app.oyagoiWorkspace != ws.ID || second.app.oyagoiWorkspace != 0 {
		t.Fatal("HQ projection was not viewer scoped")
	}
	first.SetOyagoi(app, ws.ID, "wrong")
	if first.app.oyagoiWorkspace != 0 {
		t.Fatal("wrong generation accepted")
	}
}
