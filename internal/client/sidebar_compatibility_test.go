package client

import (
	"fmt"
	"testing"

	"github.com/kazuph/goterrace/internal/render"
)

func TestSidebarPanelDisplayCompatibility(t *testing.T) {
	for _, compact := range []bool{false, true} {
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
				if hit.Machine != "remote" || !hit.Right || (!compact && hit.Row.Kind != render.RowJob) || (compact && (hit.Row.Kind != render.RowAgent || hit.Row.Pane != 2 || hit.Row.Line != 0)) {
					continue
				}
				if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
					t.Fatal(err)
				}
				h.until("compatible row action", func() bool {
					s := m.endpoints["remote"].snapshot
					if compact {
						return m.pending == nil && m.active == "remote" && s.State.FocusedPane() == 2
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
