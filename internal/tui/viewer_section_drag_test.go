package tui

import (
	"bytes"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestEndpointSectionHeaderKeepsOrderAndBodyMovesToTarget(t *testing.T) {
	st := model.New(t.TempDir(), 160, 45)
	st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true, Section: model.SectionWork})
	st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true, Section: model.SectionPersonal})
	ids := []int{st.Workspaces[0].ID, st.Workspaces[1].ID, st.Workspaces[2].ID}
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	app.SetServerUI()
	viewer := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	viewer.SetSnapshotSink(func(render.Input) error { return nil })
	viewer.with(app, func(ui *App) {
		position := func(kind render.SidebarRowKind, index int, section model.Section) int {
			for _, pos := range ui.sidebarView().Rows {
				if pos.Row.Kind == kind && (kind == render.RowWorkspace && pos.Row.Ws == index || kind == render.RowSection && pos.Row.Section == section) {
					return pos.Y
				}
			}
			t.Fatal("missing section/card hit area", kind, index, section)
			return 0
		}
		drag := func(source, target int) {
			ui.sidebarPress(uv.Mouse{X: 4, Y: source, Button: uv.MouseLeft})
			ui.sidebarGestureMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: 4, Y: target, Button: uv.MouseLeft}})
			ui.sidebarGestureMouse(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: 4, Y: target, Button: uv.MouseLeft}})
		}
		drag(position(render.RowWorkspace, 0, ""), position(render.RowSection, 0, model.SectionWork))
		for i, id := range ids {
			if ui.st.Workspaces[i].ID != id {
				t.Fatal("header drop changed workspace order")
			}
		}
		if ui.st.Workspaces[0].SidebarSection() != model.SectionWork {
			t.Fatal("header drop did not change section")
		}
		drag(position(render.RowWorkspace, 0, ""), position(render.RowWorkspace, 2, ""))
		for i, id := range []int{ids[1], ids[0], ids[2]} {
			if ui.st.Workspaces[i].ID != id {
				t.Fatal("body drop did not insert before its target")
			}
		}
		if ui.st.Workspaces[1].SidebarSection() != model.SectionPersonal || ui.st.Workspaces[0].SidebarSection() != model.SectionWork || ui.st.Workspaces[2].SidebarSection() != model.SectionPersonal {
			t.Fatal("body drop changed another workspace's section")
		}
	})
}
