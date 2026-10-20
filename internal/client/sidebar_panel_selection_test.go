package client

import (
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/render"
)

func TestSidebarContinuousDifferentEndpointPanels(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(0, 0, 240, 60)
	local := m.endpoints["local"]
	header := render.RightSidebarHeaderFor(local.snapshot.State, 0)
	raw := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", header.Jobs.X+1, header.Jobs.Y+1, header.Jobs.X+1, header.Jobs.Y+1)
	if err := m.sendInput("local", []byte(raw)); err != nil {
		t.Fatal(err)
	}
	h.until("local jobs selected while remote agents retained", func() bool {
		return local.snapshot.JobsPanel && !m.endpoints["remote"].snapshot.JobsPanel
	})
	if err := m.sendInput("remote", []byte("\x02\t")); err != nil {
		t.Fatal(err)
	}
	h.until("remote job caller deselected", func() bool { return m.endpoints["remote"].snapshot.State.FocusedPane() != 1 })
	h.modes(0, 1, 240, 60)
	for _, hit := range m.sidebar.Hits {
		if hit.Machine != "remote" || !hit.Right || hit.Row.Kind != render.RowJob {
			continue
		}
		if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
			t.Fatal(err)
		}
		h.until("compact remote job click reaches displayed caller", func() bool {
			snapshot := m.endpoints["remote"].snapshot
			return m.pending == nil && m.active == "remote" && snapshot.JobsPanel && snapshot.State.FocusedPane() == 1
		})
		return
	}
	t.Fatal("active local jobs selection did not draw the remote job")
}
