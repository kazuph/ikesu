package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/endpoint"
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
			p := endpoint.IdlePane{State: tc.state, PendingInput: tc.pending, Focused: tc.focused}
			if got := idleInitiallySelected(p, tc.hq, tc.active); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
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
