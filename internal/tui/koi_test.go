package tui

import (
	"bytes"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"reflect"
	"testing"
)

func TestKoiStandaloneClockCannotTriggerSnapshotSink(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetReported(a.focusedPane(), "codex", "working")
	a.draw()
	if !a.koiLocalActive {
		t.Fatal("standalone fish clock not active")
	}
	a.snapshotSink = func(in render.Input) error {
		if in.Koi || in.KoiTime != 0 || in.KoiGlow != nil {
			t.Fatal("decoration leaked into snapshot")
		}
		return nil
	}
	a.draw()

	off := false
	a.sidebarConfig.UI.Koi = &off
	a.snapshotSink = nil
	a.draw()
	if a.koiLocalActive {
		t.Fatal("off kept standalone animation active")
	}
}

func TestKoiServerFrameViewerDoesNotOwnDecorationClock(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetReported(a.focusedPane(), "codex", "idle")
	var on, off bytes.Buffer
	viewer := a.NewViewer(&on, 160, 45, nil)
	viewer.Draw(a, a.SessionPaneSnaps())
	disabled := false
	a.sidebarConfig.UI.Koi = &disabled
	plain := a.NewViewer(&off, 160, 45, nil)
	plain.Draw(a, a.SessionPaneSnaps())
	onScreen, offScreen := vt.NewEmulator(160, 45), vt.NewEmulator(160, 45)
	defer onScreen.Close()
	defer offScreen.Close()
	if _, err := onScreen.Write(on.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := offScreen.Write(off.Bytes()); err != nil {
		t.Fatal(err)
	}
	state := *a.st
	state.Cols, state.Rows = 160, 45
	// Live shell output may change the pane between reads. Only decoration's
	// sidebar ownership is under test; every sidebar cell must stay exact.
	for _, area := range []layout.Rect{state.SidebarArea(), state.RightSidebarArea()} {
		for y := area.Y; y < area.Y+area.H; y++ {
			for x := area.X; x < area.X+area.W; x++ {
				if !reflect.DeepEqual(onScreen.CellAt(x, y), offScreen.CellAt(x, y)) {
					t.Fatalf("server sidebar changed with client decoration setting at %d,%d", x, y)
				}
			}
		}
	}
	if viewer.app.koiLocalActive {
		t.Fatal("server frame viewer owns a client-only decoration clock")
	}
}
