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
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestKoiStandaloneClockCannotTriggerSnapshotSink(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	enabled := true
	a.sidebarConfig.UI.Koi = &enabled
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
	enabled := true
	a.sidebarConfig.UI.Koi = &enabled
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
	for _, label := range []string{"koi: off", "koi: on"} {
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
	enabled := true
	a.sidebarConfig.UI.Koi = &enabled
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
	enabled := true
	a.sidebarConfig.UI.Koi = &enabled
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

func TestKoiRev3TogglePreservesEndpointPanePalette(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, config.EnvVar, filepath.Join(t.TempDir(), "config.toml"))
	a := specRuntimeApp(t)
	a.serverUI = true
	palette := render.ThemePalette("dracula")
	a.panePalette = &palette
	a.toggleKoi()
	if a.panePalette != &palette {
		t.Fatal("koi toggle replaced the endpoint's existing pane palette")
	}
}

func TestStandaloneKittyKoiAdvancesOnDirtyDeadlineAndKeepsJobsPoseBeforeIt(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	teststate.Lock(t)
	teststate.Setenv(t, "TERM", "xterm-kitty")
	for _, key := range []string{"TMUX", "STY"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	a := specRuntimeApp(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	prevIn := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = prevIn }()
	a.out = slave
	a.SetCellPixels(a.st.Cols*8, a.st.Rows*16)
	enabled := true
	a.sidebarConfig.UI.Koi, a.sidebarConfig.UI.TransparentSidebarCards = &enabled, true
	a.sidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent"}
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetReported(a.focusedPane(), "codex", "working")
	a.jobsPanel = true
	start := time.Now()
	jobStart := start.Add(-2*time.Second + 3*render.KoiRestInterval).UnixMilli()
	a.jobRows = []render.JobRow{{ID: "j1", Label: "timer", Status: "running", AgeSinceMs: &jobStart,
		Elapsed: formatJobAge(start.UnixMilli() - jobStart)}}

	chunks := make(chan string, 16)
	go func() {
		defer close(chunks)
		p := make([]byte, 16384)
		for {
			n, err := master.Read(p)
			if n > 0 {
				select {
				case chunks <- string(p[:n]):
				case <-t.Context().Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var pending string
	readFrame := func() string {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			if wire, rest, ok := strings.Cut(pending, "\x1b[?2026l"); ok {
				pending = rest
				return wire
			}
			select {
			case chunk, ok := <-chunks:
				if !ok {
					t.Fatal("outer PTY closed before the frame")
				}
				pending += chunk
			case <-timer.C:
				t.Fatal("outer PTY did not receive the synchronized frame")
			}
		}
	}

	a.drawAt(start)
	readFrame()
	if !a.koiInput.KoiKitty || len(a.koiScene.Mini) != 1 || a.koiNext.IsZero() {
		t.Fatal("fixture needs a Kitty mini koi animation in Jobs")
	}
	placed := false
	for range 4 {
		previous := a.koiScene.Mini[0].X
		due := a.koiNext
		a.dirty = true
		a.onKoiDeadline(due)
		a.drawAt(due)
		wire := readFrame()
		placed = placed || strings.Contains(wire, "\x1b_Ga=p,")
		if len(a.koiScene.Fish) != 0 || a.koiScene.Mini[0].X >= previous || !a.koiNext.After(due) {
			t.Fatal("dirty deadline did not advance the mini koi and reschedule")
		}
	}
	if !placed {
		t.Fatal("animation did not update any Kitty placement on the PTY")
	}
	previousPose, previousTime, due := a.koiScene.Mini[0], a.koiInput.KoiTime, a.koiNext
	updateAt := due.Add(-time.Millisecond)
	a.refreshJobDots(updateAt)
	if !a.dirty || strings.TrimSpace(a.jobRows[0].Elapsed) != "2s" {
		t.Fatal("job elapsed timer did not dirty the frame")
	}
	a.drawAt(updateAt)
	wire := readFrame()
	if strings.Contains(wire, "\x1b_G") || a.koiScene.Mini[0] != previousPose ||
		a.koiInput.KoiTime != previousTime || a.koiNext != due {
		t.Fatal("job update before the deadline changed the pose, placement, or deadline")
	}
}
