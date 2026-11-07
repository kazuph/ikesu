package client

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
)

// Exercises an open viewer menu while the real server changes workspace order.
func TestOyagoiMenuIdentityAfterServerChangesActualPTY(t *testing.T) {
	for _, change := range []string{"delete", "reorder"} {
		t.Run(change, func(t *testing.T) {
			h := newRightMachineHarness(t)
			m := h.m
			m.order = []string{"local"}
			m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
			e := m.endpoints["local"]
			original, hq := e.snapshot.State.Workspaces[0], e.snapshot.State.Workspaces[1]
			if err := m.oyagoiAction("local", fmt.Sprintf("oyagoi:set:%d:%s", hq.ID, hq.Generation)); err != nil {
				t.Fatal(err)
			}
			h.until("HQ projected", func() bool { return e.snapshot.MobileOyagoi == hq.ID })
			if err := e.connection.enqueue(msg{Type: "workspace_menu", Workspace: original.ID, WorkspaceGeneration: original.Generation, MenuX: 5, MenuY: 5}); err != nil {
				t.Fatal(err)
			}
			h.until("menu opened", func() bool { return e.snapshot.Menu != nil })
			steps := 0
			for _, item := range e.snapshot.Menu.Items {
				if item.Label == "💼 work" {
					break
				}
				if !item.Separator && !item.Disabled {
					steps++
				}
			}
			if change == "delete" {
				h.api("local", api.MethodWorkspaceClose, map[string]any{"workspace_id": fmt.Sprintf("s%d", original.ID)})
				h.until("original deleted", func() bool { return len(e.snapshot.State.Workspaces) == 1 })
			} else {
				h.api("local", "workspace.move", map[string]any{"workspace_id": fmt.Sprintf("s%d", original.ID), "insert_index": 1})
				h.until("original moved", func() bool { return e.snapshot.State.Workspaces[0].ID == hq.ID })
			}
			if err := m.sendInput("local", []byte(strings.Repeat("\x1b[B", steps)+"\r")); err != nil {
				t.Fatal(err)
			}
			h.until("menu processed", func() bool { return e.snapshot.Menu == nil && e.processedInput == e.connection.lastSentInput() })
			if m.oyagoi == nil || m.oyagoi.Workspace != hq.ID || e.snapshot.State.Workspaces[0].Section != hq.Section {
				t.Fatal("unrelated HQ changed")
			}
			if change == "delete" {
				if e.snapshot.Dialog == nil {
					t.Fatal("missing workspace error")
				}
			} else if e.snapshot.State.Workspaces[1].Section != model.SectionWork {
				t.Fatal("moved workspace was not changed")
			}
		})
	}
}
