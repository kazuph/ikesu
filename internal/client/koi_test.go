package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestKoiClockStopsWhenOffEmptyHiddenOrFull(t *testing.T) {
	for _, condition := range []string{"visible", "off", "empty", "hidden", "full", "blurred", "idle"} {
		state := model.New(t.TempDir(), 160, 45)
		state.SidebarVisible = true
		state.SidebarSize = 26
		state.RightSidebarSize = 30
		panes := map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "working"}}}
		var cfg config.Config
		switch condition {
		case "idle":
			panes[1].Status.Key = "idle"
		case "off":
			off := false
			cfg.UI.Koi = &off
		case "empty":
			panes = nil
		case "hidden":
			state.SidebarHidden = true
			state.SidebarVisible = false
			state.RightSidebarMode = "hidden"
		case "full":
			panes[1].Status.Key = "idle"
			for i := 0; i < 60; i++ {
				cfg.UI.Sidebar.Agents.Rows = append(cfg.UI.Sidebar.Agents.Rows, []config.SidebarToken{{Token: "agent"}})
			}
		}
		snapshot := render.Input{State: state, Panes: panes, SidebarConfig: cfg}
		file, err := os.CreateTemp(t.TempDir(), "output")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		m := &machineClient{active: "local", order: []string{"local"}, out: file, size: tui.Winsize{Cols: 160, Rows: 45}, settings: &localSettings{config: &cfg}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
		m.blurred = condition == "blurred"
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		want := condition == "visible"
		if got := !m.koiNext.IsZero(); got != want {
			t.Fatalf("%s clock=%t want=%t", condition, got, want)
		}
		if snapshot.Koi || snapshot.KoiTime != 0 {
			t.Fatal("server snapshot mutated")
		}
	}
}

func TestKoiDeadlineYieldsToTerminalFence(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	m := &machineClient{koiNext: now.Add(-time.Millisecond), paintQueued: true}
	if !m.outputFence.Written([]byte("\x1b_Ga=p,i=1;\x1b\\"), now) {
		t.Fatal("image fence missing")
	}
	if got := m.nextDeadline(); !got.Equal(now.Add(render.TerminalStatusWait)) {
		t.Fatalf("expired koi clock causes a busy loop: got=%s fence=%s", got, m.outputFence.Deadline())
	}
	m.outputFence.Accept([]byte("\x1b[0n"))
	m.paintQueued = false
	if got := m.nextDeadline(); !got.Equal(m.koiNext) {
		t.Fatal("reply did not restore decoration deadline")
	}
}

func TestKoiDecorationTickDoesNotComposeOrAcknowledge(t *testing.T) {
	state := model.New(t.TempDir(), 160, 45)
	state.SidebarVisible = true
	state.SidebarSize = 26
	state.RightSidebarSize = 30
	snapshot := render.Input{State: state, Panes: map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "working"}}}}
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m := &machineClient{active: "local", order: []string{"local"}, out: file, koiKitty: true, size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, settings: &localSettings{}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	m.outputFence.Accept([]byte("\x1b[0n"))
	m.paintQueued = false
	regions := m.regions
	scene := m.koiScene
	before := snapshot
	if err := m.paintKoi(time.Now().Add(render.KoiInterval)); err != nil {
		t.Fatal(err)
	}
	if m.regions != regions {
		t.Fatal("tick rebuilt hit regions")
	}
	if !reflect.DeepEqual(m.koiScene, scene) || !reflect.DeepEqual(snapshot, before) {
		t.Fatal("tick mutated UI/server state")
	}
	if snapshot.Koi || snapshot.KoiMotion != nil {
		t.Fatal("client clock escaped into snapshot")
	}
	// The ordinary framed cell mouse path consumes only the vacant pond.
	m.outputFence.Accept([]byte("\x1b[0n"))
	m.paintQueued = false
	x, y := scene.Pond.X+scene.Pond.W/2, scene.Pond.Y+scene.Pond.H/2
	if err := m.input(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}}); err != nil {
		t.Fatal(err)
	}
	check := m.koiInput
	m.koiClock.Apply(&check, nil, time.Now(), tui.FossilExpiry())
	if check.KoiRipple == nil || check.KoiRipple.X != float64(x)+.5 || check.KoiRipple.Y != float64(y)+.5 {
		t.Fatal("framed mouse missed pond")
	}
	if !m.suppressRelease {
		t.Fatal("pond release would escape to server")
	}
}
