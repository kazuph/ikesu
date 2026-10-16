package server

import (
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestGraphicsPublishedSurfaceDoesNotFollowRestoredViewerState(t *testing.T) {
	state := model.New(".", 80, 24)
	pid := state.FocusedPane()
	surface := graphicsSurface(render.Input{State: state, Panes: map[int]*render.PaneSnap{pid: {
		Graphics: []kitty.Placement{{Image: kitty.Image{ID: 3, NativeID: 17}}},
	}}})
	if !surfaceHasPane(surface, pid) || !surface.native[17] {
		t.Fatal("visible pane/native image absent from captured surface", surface)
	}
	state.Workspaces = nil
	state.Cols, state.Rows = 0, 0
	if !surfaceHasPane(surface, pid) || !surface.native[17] {
		t.Fatal("published surface followed later mutable app selection", surface)
	}
}

func TestGraphicsNativeProjectionDoesNotExposeAnotherTerminalsImage(t *testing.T) {
	owner, other := &clientConn{}, &clientConn{}
	s := &Server{graphicsFiles: graphicsFileHub{streams: map[uint64]nativeGraphicsStream{
		1: {viewer: owner, image: 17}, 2: {viewer: other, image: 18},
	}}}
	in := render.Input{Panes: map[int]*render.PaneSnap{1: {Graphics: []kitty.Placement{
		{Image: kitty.Image{ID: 1, Data: []byte{1, 2, 3, 255}}},
		{Image: kitty.Image{ID: 2, NativeID: 18}},
		{Image: kitty.Image{ID: 3, NativeID: 17}},
		{Image: kitty.Image{ID: 4, NativeID: 19}},
	}}}}
	projected := s.nativeGraphicsInput(owner, in)
	got := projected.Panes[1].Graphics
	if len(got) != 2 || got[0].Image.ID != 1 || got[1].Image.NativeID != 17 {
		t.Fatal("foreign or retired terminal image leaked to viewer", got)
	}
	if len(in.Panes[1].Graphics) != 4 || in.Panes[1].Graphics[1].Image.NativeID != 18 {
		t.Fatal("projection mutated the session-owned graphics")
	}
	s.graphicsFiles.streams = nil
	got = s.nativeGraphicsInput(owner, in).Panes[1].Graphics
	if len(got) != 1 || got[0].Image.NativeID != 0 {
		t.Fatal("retired stream retained terminal-only descriptors", got)
	}
}
