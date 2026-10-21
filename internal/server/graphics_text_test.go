package server

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"testing"
)

func TestGraphicsTextGeometryAndGeneration(t *testing.T) {
	state := model.New(".", 80, 24)
	pid := state.FocusedPane()
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{pid: {Graphics: []kitty.Placement{{ID: 1, Image: kitty.Image{ID: 1, Width: 1, Height: 1, Format: 32, Data: []byte{1, 2, 3, 255}}, Cols: 1, Rows: 1}}}}}
	old := captureGraphicsText(in)
	in.Panes[pid].Graphics[0].Image.Data = []byte{4, 5, 6, 255}
	if !old.compatible(captureGraphicsText(in)) {
		t.Fatal("pixel generation blocked text")
	}
	cases := []struct {
		name   string
		change func(*kitty.Placement)
	}{
		{"source", func(p *kitty.Placement) { p.Image.ID++ }},
		{"native", func(p *kitty.Placement) { p.Image.NativeID++ }},
		{"width", func(p *kitty.Placement) { p.Image.Width++ }},
		{"height", func(p *kitty.Placement) { p.Image.Height++ }},
		{"format", func(p *kitty.Placement) { p.Image.Format = 24 }},
		{"placement", func(p *kitty.Placement) { p.ID++ }},
		{"x", func(p *kitty.Placement) { p.X++ }}, {"y", func(p *kitty.Placement) { p.Y++ }},
		{"cols", func(p *kitty.Placement) { p.Cols++ }}, {"rows", func(p *kitty.Placement) { p.Rows++ }},
		{"z", func(p *kitty.Placement) { p.Z++ }},
		{"crop x", func(p *kitty.Placement) { p.SourceX++ }}, {"crop y", func(p *kitty.Placement) { p.SourceY++ }},
		{"crop width", func(p *kitty.Placement) { p.SourceWidth++ }}, {"crop height", func(p *kitty.Placement) { p.SourceHeight++ }},
		{"offset x", func(p *kitty.Placement) { p.OffsetX++ }}, {"offset y", func(p *kitty.Placement) { p.OffsetY++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := in.Panes[pid].Graphics[0]
			tc.change(&in.Panes[pid].Graphics[0])
			if old.compatible(captureGraphicsText(in)) {
				t.Fatal("geometry accepted")
			}
			in.Panes[pid].Graphics[0] = p
		})
	}
	state.Cols++
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("resize accepted")
	}
	state.Cols--
	in.Panes[pid].ScrollOff++
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("scroll accepted")
	}
}

func TestGraphicsTextPopupAndLayoutChanges(t *testing.T) {
	state := model.New(".", 100, 40)
	state.Apply(model.CmdOpenPopup{WorkspaceID: state.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 12}})
	popup := state.Popup.PaneID
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{state.FocusedPane(): {}, popup: {Grid: make([][]uv.Cell, 10), Graphics: []kitty.Placement{{Image: kitty.Image{ID: 1, Format: 32, Width: 1, Height: 1, Data: []byte{1, 2, 3, 255}}}}}}}
	old := captureGraphicsText(in)
	in.Panes[popup].Grid[0] = []uv.Cell{{Content: "updated", Width: 1}}
	if !old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup content update blocked")
	}
	state.Popup.TerminalID = "changed"
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup terminal changed")
	}
	state.Popup.TerminalID = ""
	in.Panes[popup].ScrollOff = 1
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup scroll accepted")
	}
	in.Panes[popup].ScrollOff = 0
	in.Panes[popup].Grid = append(in.Panes[popup].Grid, nil)
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup viewport accepted")
	}
	in.Panes[popup].Grid = in.Panes[popup].Grid[:10]
	state.Popup.Width.Cells++
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup size accepted")
	}
	state.Popup.Width.Cells--
	state.Popup = nil
	if old.compatible(captureGraphicsText(in)) {
		t.Fatal("popup closure accepted")
	}
}

func TestGraphicsTextRejectsTabPaneSplitAndViewportChanges(t *testing.T) {
	cases := []struct {
		name   string
		change func(*model.AppState, render.Input, int)
	}{
		{"tab identity", func(s *model.AppState, _ render.Input, _ int) { s.ActiveWorkspace().TabRef().ID++ }},
		{"pane identity", func(s *model.AppState, _ render.Input, id int) {
			s.ActiveWorkspace().TabRef().Layout = layout.New(id + 1)
		}},
		{"split", func(s *model.AppState, _ render.Input, id int) {
			s.ActiveWorkspace().TabRef().Layout.SplitFocused(layout.Vertical, id+1)
		}},
		{"tab switch", func(s *model.AppState, _ render.Input, _ int) { s.Apply(model.CmdNewTab{Workspace: 0, Cwd: "."}) }},
		{"workspace switch", func(s *model.AppState, _ render.Input, _ int) { s.Apply(model.CmdNewWorkspace{Cwd: "."}) }},
		{"popup open", func(s *model.AppState, _ render.Input, _ int) {
			s.Apply(model.CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 12}})
		}},
		{"rows", func(s *model.AppState, _ render.Input, _ int) { s.Rows++ }},
		{"sidebar geometry", func(s *model.AppState, _ render.Input, _ int) { s.Apply(model.CmdToggleSidebar{}) }},
		{"pane viewport", func(_ *model.AppState, in render.Input, id int) { in.Panes[id].Grid = append(in.Panes[id].Grid, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := model.New(".", 100, 40)
			id := state.FocusedPane()
			in := render.Input{State: state, Panes: map[int]*render.PaneSnap{id: {Graphics: []kitty.Placement{{Image: kitty.Image{ID: 1, Format: 32, Width: 1, Height: 1, Data: []byte{1, 2, 3, 255}}}}}}}
			old := captureGraphicsText(in)
			tc.change(state, in, id)
			if old.compatible(captureGraphicsText(in)) {
				t.Fatal("changed view accepted text")
			}
		})
	}
}
