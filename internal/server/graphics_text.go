package server

import (
	"encoding/json"
	"reflect"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/render"
)

// Capture values, never pointers into the app's temporarily selected state.
// Image bytes are immutable snapshots and stay with the published generation.
type graphicsTextSurface struct {
	geometry   string
	placements map[int][]kitty.Placement
}

func captureGraphicsText(in render.Input) *graphicsTextSurface {
	hasImages := false
	for _, p := range in.Panes {
		if p != nil && len(p.Graphics) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return nil
	}
	if in.State == nil || in.State.ActiveWorkspace() == nil || in.State.ActiveWorkspace().TabRef() == nil {
		return nil
	}
	state := in.State
	ws := state.ActiveWorkspace()
	tab := ws.TabRef()
	type paneView struct{ Offset, Rows int }
	views := map[int]paneView{}
	placements := map[int][]kitty.Placement{}
	for id, p := range in.Panes {
		if p == nil {
			return nil
		}
		views[id] = paneView{p.ScrollOff, len(p.Grid)}
		placements[id] = append([]kitty.Placement(nil), p.Graphics...)
	}
	popupID := 0
	terminal := ""
	if state.PopupVisible() {
		popupID = state.Popup.PaneID
		terminal = state.Popup.TerminalID
	}
	geometry, err := json.Marshal([]any{state.Cols, state.Rows, ws.ID, tab.ID, tab.Layout.Fingerprint(), state.DisplayPaneRects(), in.Scroll, popupID, terminal, state.PopupRect(), views})
	if err != nil {
		return nil
	}
	return &graphicsTextSurface{string(geometry), placements}
}

func (old *graphicsTextSurface) compatible(next *graphicsTextSurface) bool {
	if old == nil || next == nil || old.geometry != next.geometry || len(old.placements) != len(next.placements) {
		return false
	}
	for id, previous := range old.placements {
		current, ok := next.placements[id]
		if !ok || len(previous) != len(current) {
			return false
		}
		for i, a := range previous {
			b := current[i]
			a.Image.Data = nil
			b.Image.Data = nil
			if !reflect.DeepEqual(a, b) {
				return false
			}
		}
	}
	return true
}

func (old *graphicsTextSurface) input(in render.Input) render.Input {
	panes := make(map[int]*render.PaneSnap, len(in.Panes))
	for id, p := range in.Panes {
		value := *p
		value.Graphics = append([]kitty.Placement(nil), old.placements[id]...)
		for i := range value.Graphics {
			value.Graphics[i].Image.Data = nil
		}
		panes[id] = &value
	}
	in.Panes = panes
	in.GraphicsPatches = nil
	return in
}
