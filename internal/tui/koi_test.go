package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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

func TestKoiRev3MainMenuTogglePersists(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	a := specRuntimeApp(t)
	for _, label := range []string{"koi: on", "koi: off"} {
		a.openMainMenu(0, 0)
		index := -1
		for i, item := range a.menu.m.Items {
			if item.Label == label {
				index = i
			}
		}
		if index < 0 {
			t.Fatal("menu does not show current koi state", label)
		}
		before := a.sidebarConfig.KoiEnabled()
		a.activateMenu(index)
		saved, err := config.Load()
		if err != nil || a.sidebarConfig.KoiEnabled() == before || saved.KoiEnabled() != a.sidebarConfig.KoiEnabled() {
			t.Fatal("menu/save/reload disagree", err)
		}
	}
}

func TestKoiRev3StandaloneEmptyClickHasNoAnimationSideEffect(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.draw()
	r := a.koiScene.Pond
	if r.W == 0 || r.H == 0 {
		t.Fatal("fixture needs an empty visible pond")
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: uv.MouseLeft}})
	in := render.Input{}
	a.koiClock.Apply(&in, nil, time.Now(), 15*time.Second)
	if in.KoiRipple != nil {
		t.Fatal("empty pond click started a ripple")
	}
	a.draw()
	if a.koiLocalActive {
		t.Fatal("empty click started periodic repaint")
	}
}

func TestKoiRev3MenuSaveFailureKeepsVisibleSetting(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config-is-directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, config.EnvVar, path)
	a := specRuntimeApp(t)
	a.openMainMenu(0, 0)
	index := -1
	for i, item := range a.menu.m.Items {
		if item.Label == "koi: on" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("missing koi setting")
	}
	a.activateMenu(index)
	if !a.sidebarConfig.KoiEnabled() {
		t.Fatal("failed save changed active koi configuration")
	}
}
