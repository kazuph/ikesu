package client

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

// Uses the server's real viewer and shell PTYs, with a single visible machine.
func TestOyagoiSingleMachineCycleCollapsedActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.order = []string{"local"}
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["local"]
	h.api("local", api.MethodWorkspaceCreate, map[string]any{"focus": false})
	h.until("three workspaces", func() bool { return len(e.snapshot.State.Workspaces) == 3 })
	first, hq, last := e.snapshot.State.Workspaces[0].ID, e.snapshot.State.Workspaces[1], e.snapshot.State.Workspaces[2].ID
	h.api("local", "workspace.set_section", map[string]any{"workspace_id": fmt.Sprintf("s%d", hq.ID), "section": "favorite"})
	h.api("local", "workspace.set_section", map[string]any{"workspace_id": fmt.Sprintf("s%d", first), "section": "favorite"})
	if err := m.oyagoiAction("local", fmt.Sprintf("oyagoi:set:%d:%s", hq.ID, hq.Generation)); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode([]byte("onboarding=false\n[keys]\nnext_workspace='cmd+j'\nprevious_workspace='cmd+k'\nswitch_workspace='cmd+1..9'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.connection.enqueue(msg{Type: "view_config", Config: &cfg}); err != nil {
		t.Fatal(err)
	}
	h.until("HQ and bindings", func() bool {
		return e.snapshot.MobileOyagoi == hq.ID && workspaceCycleDirection(e.snapshot.SidebarConfig, cycleKey('j').Key) == 1
	})
	selectID := func(id int) {
		t.Helper()
		if err := e.connection.enqueue(msg{Type: "workspace_select", Workspace: id}); err != nil {
			t.Fatal(err)
		}
		h.until("selection", func() bool { return e.snapshot.State.ActiveWorkspace().ID == id })
	}
	key := func(raw string, want int) {
		t.Helper()
		if err := m.sendInput("local", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		h.until("navigation receipt", func() bool { return e.processedInput == e.connection.lastSentInput() })
		if got := e.snapshot.State.ActiveWorkspace().ID; got != want {
			t.Fatalf("key %q selected %d, want %d", raw, got, want)
		}
	}
	for _, sectionCollapsed := range []bool{false, true} {
		if sectionCollapsed {
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, hit := range m.sidebar.Hits {
				if hit.Machine == "local" && hit.Row.Kind == render.RowSection && hit.Row.Section == model.SectionFavorite {
					clickCell(t, h, hit.Rect.X+2, hit.Rect.Y)
					found = true
					break
				}
			}
			if !found {
				t.Fatal("favorite heading missing")
			}
			h.until("section collapsed", func() bool { return e.snapshot.State.CollapsedSections[model.SectionFavorite] })
		}
		for step, collapsed := range []bool{false, true, false} {
			if step > 0 {
				if err := m.oyagoiStore.toggleCollapsed(); err != nil {
					t.Fatal(err)
				}
				if err := m.refreshOyagoi(); err != nil {
					t.Fatal(err)
				}
				h.until("HQ collapse projected", func() bool { return e.snapshot.MobileOyagoiCollapsed == collapsed })
			}
			selectID(last)
			want := hq.ID
			if collapsed && !sectionCollapsed {
				want = first
			}
			if collapsed && sectionCollapsed {
				want = last
			}
			key("\x1b[106;9u", want)
			selectID(last)
			key("\x1b[49;9u", want)
			selectID(last)
			wantPrev := first
			if sectionCollapsed {
				wantPrev = hq.ID
				if collapsed {
					wantPrev = last
				}
			}
			key("\x1b[107;9u", wantPrev)
		}
	}
}
