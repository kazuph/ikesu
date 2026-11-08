package client

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/runtime"
	"testing"
	"time"
)

func TestIdleInitiallySelected(t *testing.T) {
	for _, tc := range []struct {
		name, state                        string
		pending, focused, hq, active, want bool
	}{
		{"idle", "idle", false, false, false, true, true}, {"done", "done", false, false, false, true, true},
		{"working", "working", false, false, false, true, false}, {"blocked", "blocked", false, false, false, true, false}, {"unknown", "unknown", false, false, false, true, false},
		{"pending", "idle", true, false, false, true, false}, {"focused", "idle", false, true, false, true, false}, {"remote focus", "idle", false, true, false, false, true}, {"HQ", "idle", false, false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := endpoint.IdlePane{State: tc.state, PendingInput: tc.pending, InputKnown: true, Focused: tc.focused}
			if got := idleInitiallySelected(p, tc.hq, tc.active); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestIdleUnknownInputIsNotInitiallySelected(t *testing.T) {
	if idleInitiallySelected(endpoint.IdlePane{State: "idle"}, false, true) {
		t.Fatal("unrecognized prompt must not be assumed empty")
	}
}

func TestIdleUnknownInputCanBeSelectedManually(t *testing.T) {
	m := &machineClient{idle: &idleDialog{entries: []idleEntry{{pane: endpoint.IdlePane{State: "idle", AgeNanos: int64(48 * time.Hour)}}}}}
	m.idleInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: ' '}})
	if m.idle.selected() != 1 {
		t.Fatal("manual selection of an unknown input failed")
	}
}
func TestIdleThresholdAndSelection(t *testing.T) {
	d := idleDialog{entries: []idleEntry{{pane: endpoint.IdlePane{AgeNanos: int64(8 * 24 * time.Hour)}, selected: true}, {pane: endpoint.IdlePane{AgeNanos: int64(2 * 24 * time.Hour)}, selected: true}, {pane: endpoint.IdlePane{AgeNanos: int64(24 * time.Hour)}, selected: true}}}
	if got := len(d.visible()); got != 2 {
		t.Fatalf("24h: %d", got)
	}
	d.threshold = 1
	if got := len(d.visible()); got != 1 {
		t.Fatalf("3d: %d", got)
	}
	d.threshold = 2
	if got := d.selected(); got != 1 {
		t.Fatalf("1w: %d", got)
	}
}

// Two isolated server processes and their PTYs exercise the cross-machine deletion path.
func TestIdlePanesTwoServersActualPTY(t *testing.T) {
	h := newRightMachineHarness(t, true)
	m := h.m
	for _, id := range m.order {
		for pane := 1; pane <= 2; pane++ {
			h.api(id, "pane.report_agent", map[string]any{"pane_id": fmt.Sprintf("p%d", pane), "agent": "codex", "source": "test", "state": "idle"})
		}
	}
	m.openIdlePanes()
	h.until("idle lists from both servers", func() bool { return m.idle != nil && !m.idle.loading })
	if len(m.idle.entries) != 4 {
		t.Fatalf("expected both servers: %+v", m.idle.entries)
	}
	for i := range m.idle.entries {
		m.idle.entries[i].selected = m.idle.entries[i].pane.ID == 1
	}
	if m.idle.selected() != 2 {
		t.Fatalf("expected one pane per machine selected: %+v", m.idle.entries)
	}
	m.idle.confirming = true
	m.deleteIdlePanes()
	h.until("idle delete results", func() bool { return m.idle == nil })
	if m.idleToast != "deleted 2 · failed 0" {
		t.Fatal(m.idleToast)
	}
	for _, id := range m.order {
		h.until("remaining PTY", func() bool { return len(m.endpoints[id].snapshot.State.Workspaces[0].Tabs[0].Layout.PaneIDs()) == 1 })
	}
}

func TestIdleDraftSelectionFromRealPane(t *testing.T) {
	for _, tc := range []struct {
		name, agent, screen string
		want, known         bool
	}{
		{"empty Codex", "codex", "› \x1b[2mAsk Codex to do anything\x1b[0m", true, true},
		{"Codex draft", "codex", "› unfinished", false, true},
		{"Claude draft", "claude", "❯ unfinished", false, true},
		{"unknown agent", "muse", "⟩ unfinished", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sup := runtime.NewSupervisor()
			defer sup.KillAll()
			p, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			p.SetAgent(tc.agent)
			p.WithEmu(func(e *vt.Emulator) { _, _ = e.Write([]byte("\x1b[H\x1b[2J" + tc.screen)) })
			pending, known := p.UnsentAgentInput()
			if known != tc.known || idleInitiallySelected(endpoint.IdlePane{State: "idle", PendingInput: pending, InputKnown: known}, false, true) != tc.want {
				t.Fatalf("pending=%v known=%v", pending, known)
			}
		})
	}
}
