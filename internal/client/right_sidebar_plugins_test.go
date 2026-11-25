package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/sidebarplugin"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMachineRightSidebarPluginClick(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	h := newRightMachineHarnessConfigured(t, func(cfg string) string {
		dir := filepath.Join(sidebarplugin.Dir(cfg), "right")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		body := "#!/bin/sh\n# ikesu.title = Right plugin\nprintf 'plugin-width:%s\\n' \"$IKESU_SIDEBAR_COLUMNS\"\n"
		if err := os.WriteFile(filepath.Join(dir, "probe.30s.sh"), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		return "[ui.sidebar_plugins]\nenabled=true\n"
	})
	m := h.m
	h.modes(0, 0, 160, 35)
	panel := func() *render.SidebarPanel {
		return m.endpoints[m.active].snapshot.RightSidebarPanel
	}
	h.until("right plugin output was received from the endpoint", func() bool {
		p := panel()
		return p != nil && len(p.Sections) == 1 && len(p.Sections[0].Lines) == 1 &&
			strings.HasPrefix(p.Sections[0].Lines[0].Text, "plugin-width:")
	})
	wire, err := os.ReadFile(m.out.Name())
	if err != nil || !strings.Contains(string(wire), "Right plugin") || !strings.Contains(string(wire), "plugin-width:") {
		t.Fatalf("right plugin did not reach the terminal: %v", err)
	}
	press := func() {
		in := m.endpoints[m.active].snapshot
		r := render.RightSidebarPanelRect(in.State, panel())
		for _, hit := range m.sidebar.Hits {
			if hit.Right && hit.Rect.Y >= r.Y {
				t.Fatalf("machine row overlaps plugin heading: %+v", hit)
			}
		}
		for _, final := range []byte{'M', 'm'} {
			ev := mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%d%c", r.X+2, r.Y+1, final))
			if err := m.input(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	press()
	h.until("right plugin collapsed by client click", func() bool { return panel().Sections[0].Collapsed })
	press()
	h.until("right plugin reopened by client click", func() bool { return !panel().Sections[0].Collapsed })
}
