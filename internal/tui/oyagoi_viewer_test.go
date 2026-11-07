package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

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
