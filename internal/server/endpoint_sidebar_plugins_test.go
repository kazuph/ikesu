package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/sidebarplugin"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRightSidebarPluginCapabilityKeepsLegacyBottomJobClickable(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	teststate.Lock(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui.sidebar_plugins]\nenabled=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(sidebarplugin.Dir(cfg), "right")
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "probe.30s.sh"), []byte("#!/bin/sh\nprintf 'plugin body\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, config.EnvVar, cfg)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)

	old, modern := dialClient(t, dir), dialClient(t, dir)
	defer old.conn.Close()
	defer modern.conn.Close()
	legacyPeer := endpoint.Current()
	legacyPeer.Methods = slices.DeleteFunc(legacyPeer.Methods, func(method string) bool { return method == endpoint.RightSidebarPlugins })
	const rows = 18
	old.send(t, clientMsg{Type: "hello", Peer: legacyPeer, Cols: 120, Rows: rows})
	modern.send(t, clientMsg{Type: "hello", Peer: endpoint.Current(), Cols: 120, Rows: rows})
	wait := func(c *testConn, ready func(render.Input) bool) {
		t.Helper()
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool {
			return m.Type == "output" && c.lastInput != nil && ready(*c.lastInput)
		})
		if c.lastInput == nil || !ready(*c.lastInput) {
			t.Fatal("connection closed before the expected snapshot")
		}
	}
	click := func(c *testConn, r layout.Rect) {
		t.Helper()
		cursor := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", r.X+1, r.Y+1, r.X+1, r.Y+1)
		c.send(t, clientMsg{Type: "input", Data: encode64([]byte(cursor))})
	}
	wait(old, func(in render.Input) bool { return in.State.FocusedPane() == 1 })
	wait(modern, func(in render.Input) bool {
		p := in.RightSidebarPanel
		return p != nil && len(p.Sections) == 1 && len(p.Sections[0].Lines) == 1 &&
			p.Sections[0].Lines[0].Text == "plugin body"
	})
	apiOK(t, dir, api.MethodPaneReportAgent, map[string]any{
		"pane_id": "p1", "agent": "codex", "source": "codex", "session_id": "sidebar-compat", "state": "idle",
	}, nil)
	for i := range rows - 2 {
		apiOK(t, dir, api.MethodRunStart, api.RunStartParams{Label: fmt.Sprintf("compat-job-%02d", i),
			CallerPane: "p1", Argv: []string{"/usr/bin/true"}}, nil)
	}
	for _, c := range []*testConn{old, modern} {
		click(c, render.RightSidebarHeaderFor(c.lastInput.State, 0).Jobs)
		wait(c, func(in render.Input) bool { return in.JobsPanel && len(in.Jobs) == rows-2 })
	}

	right := modern.lastInput.RightSidebarPanel
	click(modern, render.RightSidebarPanelRect(modern.lastInput.State, right))
	folded := func(in render.Input) bool {
		return in.RightSidebarPanel != nil && in.RightSidebarPanel.Sections[0].Collapsed
	}
	wait(modern, folded)
	heading := render.RightSidebarPanelRect(modern.lastInput.State, modern.lastInput.RightSidebarPanel)
	if heading.Y != rows-1 || heading.H != 1 {
		t.Fatalf("modern collapsed heading = %+v", heading)
	}
	if old.lastInput.RightSidebarPanel != nil {
		t.Fatal("server sent the right panel to a client without capability")
	}
	in := *old.lastInput
	view := render.SidebarViewForInput(in)
	last := view.Rows[len(view.Rows)-1]
	if !last.Right || last.Row.Kind != render.RowJob || last.Y != heading.Y {
		t.Fatalf("legacy bottom row = %+v, modern heading = %+v", last, heading)
	}
	want := "job log: " + in.Jobs[last.Row.Line].Label
	click(old, heading)
	wait(old, func(in render.Input) bool { return in.State.PopupVisible() && in.State.Popup.Title == want })

	// A new snapshot after the legacy click must keep the same shared fold.
	modern.send(t, clientMsg{Type: "resize", Cols: 121, Rows: rows})
	wait(modern, func(in render.Input) bool { return in.State.Cols == 121 })
	if !folded(*modern.lastInput) {
		t.Fatal("legacy job click toggled modern plugin")
	}
}
