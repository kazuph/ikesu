package client

import (
	"os"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineSidebarWheelIgnoresHorizontalAndClamps(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	state := model.New(t.TempDir(), 120, 30)
	for i := 0; i < 30; i++ {
		state.Apply(model.CmdNewWorkspace{Name: "ws", Section: model.SectionPersonal})
	}
	snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
	m := &machineClient{
		active:   "local",
		order:    []string{"local", "remote"},
		out:      out,
		settings: &localSettings{},
		size:     tui.Winsize{Cols: 120, Rows: 30},
		endpoints: map[string]*managedEndpoint{
			"local":  {profile: machine.Profile{ID: "local", Enabled: true}, snapshot: snapshot},
			"remote": {profile: machine.Profile{ID: "remote", Enabled: true}, snapshot: snapshot},
		},
	}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if m.sidebar.MaxScroll <= 0 {
		t.Fatalf("expected scrollable sidebar, MaxScroll=%d", m.sidebar.MaxScroll)
	}

	left := state.SidebarArea()
	x, y := left.X+2, left.Y+left.H-1 // bottom row (outside Hits)

	// Horizontal trackpad wheel ticks must NOT scroll down or vibrate.
	for _, btn := range []uv.MouseButton{uv.MouseWheelLeft, uv.MouseWheelRight} {
		if err := m.input(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: x, Y: y, Button: btn}}); err != nil {
			t.Fatal(err)
		}
		if m.sidebar.Scroll != 0 {
			t.Fatalf("horizontal wheel %v changed Scroll to %d", btn, m.sidebar.Scroll)
		}
	}

	// Fast wheel down during inputBatch must clamp at MaxScroll so scrolling back up responds immediately.
	m.inputBatch = true
	for i := 0; i < 200; i++ {
		if err := m.input(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseWheelDown}}); err != nil {
			t.Fatal(err)
		}
	}
	if m.sidebar.Scroll != m.sidebar.MaxScroll {
		t.Fatalf("Scroll=%d, want clamped MaxScroll=%d", m.sidebar.Scroll, m.sidebar.MaxScroll)
	}
	m.inputBatch = false

	// Scrolling up interleaved with horizontal trackpad ticks must reach 0 smoothly.
	for i := 0; i < 200; i++ {
		if err := m.input(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseWheelUp}}); err != nil {
			t.Fatal(err)
		}
		if err := m.input(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseWheelLeft}}); err != nil {
			t.Fatal(err)
		}
	}
	if m.sidebar.Scroll != 0 {
		t.Fatalf("Scroll=%d after scrolling up with horizontal ticks, want 0", m.sidebar.Scroll)
	}
}

func TestMachineSidebarSectionToggleAndNewWhenScrolled(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	state := model.New(t.TempDir(), 120, 24)
	state.Workspaces[0].Section = model.SectionNone
	for i := 0; i < 20; i++ {
		state.Apply(model.CmdNewWorkspace{Name: "personal-ws", Section: model.SectionPersonal})
	}
	state.Apply(model.CmdToggleSection{Section: model.SectionNone})

	conn := &endpointConnection{
		writer: &writer{
			available: endpoint.Generation,
			methods:   endpoint.Current().Methods,
		},
		wake: make(chan struct{}, 8),
	}
	snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
	m := &machineClient{
		active:   "local",
		order:    []string{"local", "remote"},
		out:      out,
		settings: &localSettings{},
		size:     tui.Winsize{Cols: 120, Rows: 24},
		endpoints: map[string]*managedEndpoint{
			"local":  {profile: machine.Profile{ID: "local", Enabled: true}, snapshot: snapshot, connection: conn},
			"remote": {profile: machine.Profile{ID: "remote", Enabled: true}, snapshot: snapshot},
		},
	}
	// Scroll to the bottom so `▸ spaces [new]` is visible in client Hits while outside server viewport (SidebarScroll=0).
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	m.sidebar.Scroll = m.sidebar.MaxScroll
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}

	var spacesHit *render.MachineHit
	for i := range m.sidebar.Hits {
		h := &m.sidebar.Hits[i]
		if !h.Right && h.Row.Kind == render.RowSection && h.Row.Section == model.SectionNone {
			spacesHit = h
			break
		}
	}
	if spacesHit == nil {
		t.Fatal("spaces section hit not found after scrolling")
	}

	// Clear any rendered messages enqueued by paint() before our click.
	conn.queueMu.Lock()
	conn.queue = conn.queue[:0]
	conn.queueMu.Unlock()

	// Click on `▸ spaces` label -> enqueues section_toggle with section="none".
	if err := m.input(input.Event{
		Kind:  input.KindMouseClick,
		Mouse: uv.Mouse{X: spacesHit.Rect.X + 2, Y: spacesHit.Rect.Y, Button: uv.MouseLeft},
	}); err != nil {
		t.Fatalf("section toggle click failed: %v", err)
	}
	conn.queueMu.Lock()
	if len(conn.queue) != 1 {
		conn.queueMu.Unlock()
		t.Fatalf("queue len=%d, want 1", len(conn.queue))
	}
	gotToggle := conn.queue[0]
	conn.queue = conn.queue[:0]
	conn.queueMu.Unlock()
	if gotToggle.Type != "section_toggle" || gotToggle.Section == nil || *gotToggle.Section != string(model.SectionNone) {
		t.Fatalf("got %+v, want section_toggle section=none", gotToggle)
	}

	// Click on `[new]` on the `▸ spaces [new]` row -> enqueues workspace_new with section="none".
	newRect := render.SectionNewRect(spacesHit.Rect.X, spacesHit.Rect.Y, spacesHit.Rect.W)
	if newRect.W == 0 {
		t.Fatal("expected non-empty SectionNewRect")
	}
	if err := m.input(input.Event{
		Kind:  input.KindMouseClick,
		Mouse: uv.Mouse{X: newRect.X + 1, Y: newRect.Y, Button: uv.MouseLeft},
	}); err != nil {
		t.Fatalf("section [new] click failed: %v", err)
	}
	conn.queueMu.Lock()
	if len(conn.queue) != 1 {
		conn.queueMu.Unlock()
		t.Fatalf("queue len=%d, want 1", len(conn.queue))
	}
	gotNew := conn.queue[0]
	conn.queueMu.Unlock()
	if gotNew.Type != "workspace_new" || gotNew.Section == nil || *gotNew.Section != string(model.SectionNone) {
		t.Fatalf("got %+v, want workspace_new section=none", gotNew)
	}

	// If the endpoint advertises only one of section_toggle / workspace_new,
	// clicking [new] must notify unsupported and enqueue neither workspace_new
	// nor raw coordinate input.
	for _, partial := range [][]string{
		{"rendered", "input", "section_toggle"},
		{"rendered", "input", "workspace_new"},
	} {
		conn.writer.methods = partial
		m.notice = ""
		conn.queueMu.Lock()
		conn.queue = conn.queue[:0]
		conn.queueMu.Unlock()
		if err := m.input(input.Event{
			Kind:  input.KindMouseClick,
			Mouse: uv.Mouse{X: newRect.X + 1, Y: newRect.Y, Button: uv.MouseLeft},
			Raw:   []byte("\x1b[<0;1;1M"),
		}); err != nil {
			t.Fatalf("partial %v click returned unexpected error: %v", partial, err)
		}
		if m.notice == "" {
			t.Fatalf("partial %v did not set unsupported notice", partial)
		}
		conn.queueMu.Lock()
		for _, queued := range conn.queue {
			if queued.Type == "workspace_new" || queued.Type == "input" {
				conn.queueMu.Unlock()
				t.Fatalf("partial %v sent unexpected fallback message: %+v", partial, queued)
			}
		}
		conn.queueMu.Unlock()
	}
}
