package server

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"testing"
)

func TestGraphicsRelayoutKeepsPublishedGenerationAtCurrentAnchors(t *testing.T) {
	state := model.New(".", 106, 38)
	id := state.FocusedPane()
	published := kitty.Placement{ID: 2, Image: kitty.Image{ID: 1, Width: 8, Height: 8, Format: 32, Data: bytes.Repeat([]byte{1}, 8*8*4)}, X: 2, Y: 3, Cols: 4, Rows: 2, SourceWidth: 8, SourceHeight: 8}
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{id: {Graphics: []kitty.Placement{published}}}}
	old := captureGraphicsText(in)
	current := published
	current.X, current.Y, current.Cols, current.Rows = 7, 9, 8, 4
	current.Image.Width, current.Image.Height = 16, 16
	current.Image.Data = bytes.Repeat([]byte{2}, 16*16*4)
	in.Panes[id].Graphics = []kitty.Placement{current, {ID: 3, Image: kitty.Image{ID: 9}}}
	in.Panes[id].Grid = [][]uv.Cell{{{Content: "latest", Width: 1}}}
	state.Cols, state.Rows = 160, 45
	if !old.resized(captureGraphicsText(in)) {
		t.Fatal("real size change was not accepted")
	}
	in.Panes[id].ScrollOff = 3
	if old.resized(captureGraphicsText(in)) {
		t.Fatal("scrolling escaped the existing receipt wait")
	}
	in.Panes[id].ScrollOff = 0
	state.ActiveWorkspace().TabRef().ID++
	if old.resized(captureGraphicsText(in)) {
		t.Fatal("a tab change was mistaken for a resize")
	}
	state.ActiveWorkspace().TabRef().ID--
	got := old.relayoutInput(in)
	if got.State != state || got.Panes[id].Grid[0][0].Content != "latest" || len(got.Panes[id].Graphics) != 1 {
		t.Fatal("latest geometry/text or image identity was lost")
	}
	image := got.Panes[id].Graphics[0]
	if image.X != 7 || image.Y != 9 || image.Cols != published.Cols || image.Rows != published.Rows || image.Image.Width != 8 || image.Image.Height != 8 || image.SourceWidth != 8 || image.SourceHeight != 8 || len(image.Image.Data) != 0 {
		t.Fatal("pending pixels or geometry mixed with published generation", image)
	}
	if current.Image.Width != in.Panes[id].Graphics[0].Image.Width || !bytes.Equal(old.placements[id][0].Image.Data, published.Image.Data) {
		t.Fatal("projection changed a source generation")
	}
	for _, boundary := range []kitty.Placement{
		{ID: published.ID, Image: published.Image, Alternate: true},
		{ID: published.ID, Image: published.Image, Virtual: true},
		{ID: published.ID, Image: published.Image, APILayer: true},
	} {
		in.Panes[id].Graphics = []kitty.Placement{boundary}
		if len(old.relayoutInput(in).Panes[id].Graphics) != 0 {
			t.Fatal("retained generation crossed a surface boundary", boundary)
		}
	}
	in.Panes[id].Graphics = nil
	if !old.resized(captureGraphicsGeometry(in)) {
		t.Fatal("a concurrent image removal blocked a real size change")
	}
	if len(old.relayoutInput(in).Panes[id].Graphics) != 0 {
		t.Fatal("removed placement remained visible")
	}
}

func TestGraphicsRelayoutNeverMovesAnImageToAnotherPopupTerminal(t *testing.T) {
	state := model.New(".", 160, 45)
	state.Apply(model.CmdOpenPopup{WorkspaceID: state.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 12}})
	id := state.Popup.PaneID
	state.Popup.TerminalID = "first"
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{id: {Graphics: []kitty.Placement{{ID: 1, Image: kitty.Image{ID: 1}}}}}}
	old := captureGraphicsText(in)
	state.Popup.TerminalID = "second"
	if len(old.relayoutInput(in).Panes[id].Graphics) != 0 {
		t.Fatal("a prior terminal's image crossed the terminal boundary")
	}
}

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
