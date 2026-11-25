package client

import (
	"bytes"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
	"os"
	"reflect"
	"regexp"
	"strconv"
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
		koi := condition != "off"
		cfg.UI.Koi = &koi
		switch condition {
		case "idle":
			panes[1].Status.Key = "idle"
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
		want := condition == "visible" || condition == "full" || condition == "idle"
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
	koi := true
	snapshot.SidebarConfig.UI.Koi = &koi
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m := &machineClient{active: "local", order: []string{"local"}, out: file, koiKitty: true, size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, settings: &localSettings{}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	// The first frame starts outside the pond; advance until its first crop
	// uploads a real image, then exercise the unchanged completion fence.
	timer := time.NewTimer(render.KoiInterval)
	<-timer.C
	if err := m.paintKoi(time.Now()); err != nil {
		t.Fatal(err)
	}
	beforeWait, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// A real freshly uploaded frame already has a completion fence. Twenty
	// animation ticks must produce no writes or growing placement queue.
	for i := 0; i < 20; i++ {
		if err := m.paintKoi(time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	afterWait, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if beforeWait.Size() != afterWait.Size() || !m.paintQueued {
		t.Fatal("waiting fence accumulated koi output")
	}
	m.outputFence.Accept([]byte("\x1b[0n"))
	m.paintQueued = false
	regions := m.regions
	scene := m.koiScene
	before := snapshot
	timer.Reset(render.KoiInterval)
	<-timer.C
	if err := m.paintKoi(time.Now()); err != nil {
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

func TestKoiRev3PendingResizeHoldsDecorations(t *testing.T) {
	state := model.New(t.TempDir(), 160, 45)
	state.SidebarVisible = true
	state.SidebarSize = 26
	state.RightSidebarSize = 30
	snapshot := render.Input{State: state, Panes: map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "working"}}}}
	koi := true
	snapshot.SidebarConfig.UI.Koi = &koi
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
	before, _ := file.Seek(0, 1)
	m.size = tui.Winsize{Cols: 140, Rows: 40, Xpixel: 1120, Ypixel: 640}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := m.paintKoi(time.Now().Add(time.Duration(i) * render.KoiInterval)); err != nil {
			t.Fatal(err)
		}
	}
	wire, _ := os.ReadFile(file.Name())
	if bytes.Contains(wire[before:], []byte("\x1b_G")) {
		t.Fatal("pending resize repositions koi using old pond")
	}
	snapshot.State.Cols, snapshot.State.Rows = 140, 40
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if m.koiNext.IsZero() {
		t.Fatal("matching dimensions did not resume koi")
	}
}

func TestKoiRev3EmptyClicksNeverStartAnimation(t *testing.T) {
	for _, condition := range []string{"empty", "capacity", "jobs", "off"} {
		t.Run(condition, func(t *testing.T) {
			state := model.New(t.TempDir(), 160, 45)
			state.SidebarVisible = true
			state.SidebarSize = 26
			state.RightSidebarSize = 30
			var cfg config.Config
			koi := condition != "off"
			cfg.UI.Koi = &koi
			snapshot := render.Input{State: state, Panes: map[int]*render.PaneSnap{}, SidebarConfig: cfg}
			if condition != "empty" {
				snapshot.Panes[1] = &render.PaneSnap{Agent: "codex", Status: render.StatusGlyph{Key: "idle"}}
			}
			if condition == "capacity" {
				state.Rows = 6
			}
			if condition == "jobs" {
				snapshot.JobsPanel = true
			}
			file, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			m := &machineClient{active: "local", order: []string{"local"}, out: file, size: tui.Winsize{Cols: state.Cols, Rows: state.Rows}, settings: &localSettings{config: &cfg}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			if len(m.koiScene.Fish) != 0 {
				t.Fatal("ineligible fixture unexpectedly contains fish", condition, m.koiScene.Pond)
			}
			r := m.koiScene.Pond
			x, y := r.X+r.W/2, r.Y+r.H/2
			if err := m.input(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}}); err != nil {
				t.Fatal(err)
			}
			check := m.koiInput
			m.koiClock.Apply(&check, nil, time.Now(), tui.FossilExpiry())
			if check.KoiRipple != nil || !m.koiNext.IsZero() {
				t.Fatalf("ineligible click started ripple/deadline %s ripple=%+v deadline=%v scene=%+v", condition, check.KoiRipple, m.koiNext, m.koiScene)
			}
		})
	}
}

func TestKoiOctantTickSchedulesFutureDeadline(t *testing.T) {
	m, _ := koiRev3Client(t)
	m.outputFence.Accept([]byte("\x1b[0n"))
	m.paintQueued = false
	m.koiKitty = false
	m.koiInput.KoiKitty = false
	now := time.Now()
	m.koiNext = now.Add(-time.Millisecond)
	if err := m.paintKoi(now); err != nil {
		t.Fatal(err)
	}
	if !m.koiNext.After(now) {
		t.Fatal("expired koi deadline starves reconnect timers", m.koiNext, now)
	}
}

func TestJobsSnapshotPaintDoesNotReplaceMiniKoi(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%t", multi), func(t *testing.T) {
			enabled := true
			var cfg config.Config
			cfg.Theme.Custom = map[string]string{"panel_bg": "transparent"}
			cfg.UI.Koi, cfg.UI.TransparentSidebarCards = &enabled, true
			snapshot := func() *render.Input {
				state := model.New(t.TempDir(), 160, 45)
				state.SidebarVisible, state.SidebarSize, state.RightSidebarSize = true, 26, 30
				return &render.Input{State: state, SidebarConfig: cfg, ServerUI: true,
					Panes: map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "working"}}},
					Jobs:  []render.JobRow{{Label: "timer", Status: "running", Elapsed: "1s"}}}
			}
			file, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			m := &machineClient{active: "local", order: []string{"local"}, out: file, koiKitty: true,
				size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, settings: &localSettings{},
				endpoints: map[string]*managedEndpoint{"local": {snapshot: snapshot(), profile: machine.Profile{ID: "local", Enabled: true}}}}
			if multi {
				m.order = append(m.order, "remote")
				m.endpoints["remote"] = &managedEndpoint{snapshot: snapshot(), profile: machine.Profile{ID: "remote", Enabled: true}}
			}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			for i := 1; i <= 3; i++ {
				if err := m.paintKoi(now.Add(time.Duration(i) * render.KoiInterval)); err != nil {
					t.Fatal(err)
				}
				m.outputFence.Accept([]byte("\x1b[0n"))
			}
			if len(m.koiScene.Fish) == 0 || len(m.koiScene.Mini) == 0 {
				t.Fatal("fixture must include both kinds of koi")
			}
			updatedSnapshot := func(id, elapsed string, jobs bool) {
				t.Helper()
				next := *m.endpoints[id].snapshot
				next.JobsPanel = jobs
				next.Jobs = append([]render.JobRow(nil), next.Jobs...)
				next.Jobs[0].Elapsed = elapsed
				if _, done := m.event(endpointEvent{id: id, message: msg{Type: "snapshot", snapshot: &next}}); done {
					t.Fatal("client ended on snapshot")
				}
			}
			offset := func() int64 {
				t.Helper()
				n, err := file.Seek(0, 1)
				if err != nil {
					t.Fatal(err)
				}
				return n
			}
			since := func(at int64) []byte {
				t.Helper()
				data, err := os.ReadFile(file.Name())
				if err != nil {
					t.Fatal(err)
				}
				return data[at:]
			}
			at := offset()
			updatedSnapshot("local", "1s", true)
			if data := since(at); !bytes.Contains(data, []byte("a=d,")) || bytes.Contains(data, []byte("a=p,")) {
				t.Fatal("opening Jobs did not delete just the right koi")
			}
			at = offset()
			updatedSnapshot("local", "2s", true)
			if multi {
				updatedSnapshot("remote", "3s", false)
				updatedSnapshot("remote", "4s", true)
			}
			if data := since(at); bytes.Contains(data, []byte("\x1b_G")) {
				t.Fatal("elapsed-time snapshot changed graphics")
			}
			if multi {
				m.active = "remote"
				if err := m.paint(); err != nil {
					t.Fatal(err)
				}
			}
			if !m.koiInput.JobsPanel || len(m.koiScene.Fish) != 0 {
				t.Fatal("Jobs view has a right koi after repaint")
			}
			at = offset()
			if err := m.paintKoi(now.Add(5 * render.KoiInterval)); err != nil {
				t.Fatal(err)
			}
			for _, match := range regexp.MustCompile("\x1b\\[[0-9]+;([0-9]+)H\x1b_Ga=p,").FindAllSubmatch(since(at), -1) {
				x, _ := strconv.Atoi(string(match[1]))
				if x > m.koiScene.Pond.X {
					t.Fatal("decoration tick restored a right koi in Jobs")
				}
			}
		})
	}
}
