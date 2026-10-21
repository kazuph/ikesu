package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func mouseEvent(t *testing.T, raw string) input.Event {
	t.Helper()
	var framer input.Framer
	events := framer.Push([]byte(raw))
	if len(events) != 1 {
		t.Fatalf("mouse framing: %+v", events)
	}
	return events[0]
}

func TestSavedMachineSidebarCellAndPixelSelectSameWorkspace(t *testing.T) {
	for _, pixel := range []bool{false, true} {
		out, err := os.CreateTemp(t.TempDir(), "viewer")
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		peer := endpoint.Current()
		connection := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
		state := model.New(t.TempDir(), 120, 45)
		snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
		m := &machineClient{active: "local", order: []string{"local", "remote"}, out: out, settings: &localSettings{}, size: tui.Winsize{Cols: 120, Rows: 45, Xpixel: 960, Ypixel: 720}, endpoints: map[string]*managedEndpoint{
			"local":  {profile: machine.Profile{ID: "local", Enabled: true}, snapshot: snapshot},
			"remote": {profile: machine.Profile{ID: "remote", Enabled: true}, snapshot: snapshot, connection: connection},
		}}
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		var hit *render.MachineHit
		for index := range m.sidebar.Hits {
			candidate := &m.sidebar.Hits[index]
			if candidate.Machine == "remote" && candidate.Workspace != 0 {
				hit = candidate
				break
			}
		}
		if hit == nil {
			t.Fatal("remote workspace was not drawn")
		}
		x, y := hit.Rect.X+1, hit.Rect.Y+1
		if pixel {
			m.mouse.Written([]byte("\x1b[?10"))
			m.mouse.Written([]byte("16h"))
			if !m.mouse.pixel {
				t.Fatal("split pixel mode write not tracked")
			}
			x, y = (x-1)*8+2, (y-1)*16+5
		}
		if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", x, y))); err != nil {
			t.Fatal(err)
		}
		if m.pending == nil || m.pending.id != "remote" || m.pending.workspace != hit.Workspace {
			t.Fatalf("pixel=%v selected=%+v expected workspace=%d", pixel, m.pending, hit.Workspace)
		}
	}
}

func TestSavedMachinePixelBodyPreservesTwoPointsInSameCell(t *testing.T) {
	reader, writerFile, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writerFile.Close()
	peer := endpoint.Current()
	connection := &endpointConnection{writer: &writer{conn: writerFile, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- connection.writeQueued(ctx) }()
	defer func() { cancel(); <-done }()
	state := model.New(t.TempDir(), 120, 45)
	m := &machineClient{active: "local", settings: &localSettings{}, size: tui.Winsize{Cols: 120, Rows: 45, Xpixel: 960, Ypixel: 720}, regions: &render.Output{}, endpoints: map[string]*managedEndpoint{"local": {connection: connection, snapshot: &render.Input{State: state}}}}
	m.mouse.Written([]byte("\x1b[?1016h"))
	decoder := json.NewDecoder(reader)
	for _, raw := range []string{"\x1b[<0;482;325M", "\x1b[<0;486;329M"} {
		if err := m.input(mouseEvent(t, raw)); err != nil {
			t.Fatal(err)
		}
		if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var message msg
		if err := decoder.Decode(&message); err != nil {
			t.Fatal(err)
		}
		got, err := base64.StdEncoding.DecodeString(message.Data)
		if err != nil || message.Type != "input" || !bytes.Equal(got, []byte(raw)) {
			t.Fatalf("pixel bytes changed: %+v %q %v", message, got, err)
		}
	}
}

func TestSavedMachineSidebarRowTranslationRetainsPixelOffset(t *testing.T) {
	state := model.New(t.TempDir(), 120, 45)
	snapshot := &render.Input{State: state}
	target := &managedEndpoint{snapshot: snapshot, connection: &endpointConnection{}}
	m := &machineClient{size: tui.Winsize{Cols: 120, Rows: 45, Xpixel: 960, Ypixel: 720}, endpoints: map[string]*managedEndpoint{"remote": target}}
	m.mouse.Written([]byte("\x1b[?1016h"))
	view := render.SidebarViewForInput(*snapshot)
	if len(view.Rows) == 0 {
		t.Fatal("no sidebar rows")
	}
	position := view.Rows[0]
	for _, pixel := range []bool{false, true} {
		target.mouse.pixel = pixel
		event := m.mouse.CellEvent(mouseEvent(t, "\x1b[<0;10;69M"), m.size)
		got, err := m.sidebarBytes(render.MachineHit{Machine: "remote", Row: position.Row}, event)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("\x1b[<0;2;%dM", position.Y+1)
		if pixel {
			want = fmt.Sprintf("\x1b[<0;10;%dM", position.Y*16+5)
		}
		if string(got) != want {
			t.Fatalf("pixel=%v got=%q want=%q", pixel, got, want)
		}
	}
	m.mouse.Written([]byte("\x1b[?1016l"))
	target.mouse.pixel = true
	event := m.mouse.CellEvent(mouseEvent(t, "\x1b[<0;2;5M"), m.size)
	got, err := m.sidebarBytes(render.MachineHit{Machine: "remote", Row: position.Row}, event)
	want := fmt.Sprintf("\x1b[<0;13;%dM", position.Y*16+9)
	if err != nil || string(got) != want {
		t.Fatalf("cell to pixel sidebar translation: got=%q want=%q err=%v", got, want, err)
	}
}

func TestMachineSwitchRetainsMouseUnitsUntilCardRelease(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	state := model.New(t.TempDir(), 160, 45)
	e := &managedEndpoint{snapshot: &render.Input{State: state}}
	m := &machineClient{active: "remote", out: out, size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, regions: &render.Output{}, endpoints: map[string]*managedEndpoint{"remote": e}, drag: &machineDrag{id: "remote", y: 10, drop: -1}, suppressRelease: true}
	m.mouse.Written([]byte("\x1b[?1016h"))
	if _, done := m.event(endpointEvent{id: "remote", message: msg{Type: "output", Data: base64.StdEncoding.EncodeToString([]byte("\x1b[?1016l"))}}); done {
		t.Fatal("mode output closed viewer")
	}
	if err := m.setMouseMode(false); err != nil {
		t.Fatal(err)
	}
	if !m.mouse.pixel {
		t.Fatal("switch changed coordinate units before the queued button release")
	}
	if err := m.input(mouseEvent(t, "\x1b[<0;45;169m")); err != nil {
		t.Fatal(err)
	}
	if m.drag != nil || m.suppressRelease {
		t.Fatal("valid pixel release was discarded as outside the cell viewport")
	}
	if m.mouse.pixel {
		t.Fatal("released viewer did not synchronize the selected endpoint's cell mode")
	}
}
