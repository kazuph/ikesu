package client

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/endpoint"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/render"
)

func TestSidebarPanelJobsDisplayCompatibility(t *testing.T) {
	for _, compact := range []bool{false} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			h := newRightMachineHarness(t)
			m := h.m
			h.modes(0, 0, 240, 60)
			if !compact {
				header := render.RightSidebarHeaderFor(m.endpoints["local"].snapshot.State, 0)
				raw := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", header.Jobs.X+1, header.Jobs.Y+1, header.Jobs.X+1, header.Jobs.Y+1)
				if err := m.sendInput("local", []byte(raw)); err != nil {
					t.Fatal(err)
				}
				h.until("local jobs", func() bool { return m.endpoints["local"].snapshot.JobsPanel })
			} else {
				h.modes(0, 1, 240, 60)
			}
			m.outputFence.Accept([]byte("\x1b[0n"))
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			for _, hit := range m.sidebar.Hits {
				if hit.Machine != "remote" || !hit.Right || (!compact && hit.Row.Kind != render.RowJob) || (compact && (hit.Row.Kind != render.RowAgent || hit.Row.Pane != 3 || hit.Row.Line != 0)) {
					continue
				}
				if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
					t.Fatal(err)
				}
				h.until("compatible row action", func() bool {
					s := m.endpoints["remote"].snapshot
					if compact {
						return m.pending == nil && m.active == "remote" && s.State.FocusedPane() == 3
					}
					return m.pending == nil && m.active == "remote" && s.JobsPanel && s.State.PopupVisible()
				})
				t.Logf("PASS compact=%t remote methods=%v", compact, m.endpoints["remote"].connection.writer.methods)
				return
			}
			t.Fatal("expected remote row missing")
		})
	}
}

func TestSidebarPanelDisplayCompatibility(t *testing.T) {
	for _, width := range []int{2, 3, 5, 10, 17, 26} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			h := newRightMachineHarness(t)
			m := h.m
			h.modes(0, 0, 240, 60)
			for _, e := range m.endpoints {
				pref := endpoint.ViewPreferences{SidebarWidth: 26, RightSidebarWidth: width, RightSidebarManual: true}
				if width < 18 {
					pref.RightSidebarMode = "compact"
				}
				if err := e.connection.enqueue(msg{Type: "view_config", Config: m.settings.config, Preferences: &pref}); err != nil {
					t.Fatal(err)
				}
			}
			h.until("compatibility width", func() bool {
				for _, e := range m.endpoints {
					if e.snapshot.State.RightSidebarArea().W != width {
						return false
					}
				}
				return true
			})
			e := m.endpoints["remote"]
			before := e.snapshot.State.FocusedPane()
			ws := e.snapshot.State.ActiveWorkspace().ID
			if before == 3 || !e.snapshot.State.ActiveWorkspace().TabRef().Layout.HasPane(3) {
				t.Fatal("p3 is not an unselected pane in the same workspace", before, ws)
			}
			hitP3 := func() render.MachineHit {
				m.outputFence.Accept([]byte("\x1b[0n"))
				if err := m.paint(); err != nil {
					t.Fatal(err)
				}
				for _, hit := range m.sidebar.Hits {
					if hit.Right && !hit.Header && hit.Machine == "remote" && hit.Row.Kind == render.RowAgent && hit.Row.Pane == 3 && hit.Row.Line == 0 {
						return hit
					}
				}
				t.Fatal("p3 row missing")
				return render.MachineHit{}
			}
			click := func(button int) {
				hit := hitP3()
				if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<%d;%d;%dM", button, hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
					t.Fatal(err)
				}
			}
			click(0)
			h.until("same workspace p3 selected", func() bool {
				return m.pending == nil && m.active == "remote" && e.snapshot.State.FocusedPane() == 3 && e.snapshot.State.ActiveWorkspace().ID == ws
			})
			m.suppressRelease = false
			if err := m.sendInput("remote", []byte("\x02\t")); err != nil {
				t.Fatal(err)
			}
			h.until("p3 deselected before right click", func() bool { return e.snapshot.State.FocusedPane() != 3 })
			unchanged := e.snapshot.State.PaneLabels[1]
			click(2)
			h.until("p3 context menu", func() bool { return e.snapshot.Menu != nil && e.snapshot.State.FocusedPane() == 3 })
			steps, found := 0, false
			for _, item := range e.snapshot.Menu.Items {
				if item.Label == "Rename pane" {
					found = true
					break
				}
				if !item.Separator && !item.Disabled {
					steps++
				}
			}
			if !found {
				t.Fatal("pane rename action missing")
			}
			if err := m.sendInput("remote", []byte(strings.Repeat("\x1b[B", steps)+"\r")); err != nil {
				t.Fatal(err)
			}
			h.until("pane rename dialog", func() bool { return e.snapshot.Dialog != nil && e.snapshot.Dialog.Title == "Rename pane" })
			label := fmt.Sprintf("COMPAT_P3_%d", width)
			raw := strings.Repeat("\x7f", len([]rune(e.snapshot.Dialog.Input))) + label + "\r"
			if err := m.sendInput("remote", []byte(raw)); err != nil {
				t.Fatal(err)
			}
			h.until("menu action belongs to p3", func() bool {
				return e.snapshot.Dialog == nil && e.snapshot.State.PaneLabels[3] == label && e.snapshot.State.PaneLabels[1] == unchanged
			})
			t.Logf("PASS width=%d workspace=%d before=%d after=3 menu_owner=3 methods=%v", width, ws, before, e.connection.writer.methods)
		})
	}
}
