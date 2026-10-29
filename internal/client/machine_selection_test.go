package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestPaintedSelectionIgnoresFencedHiddenAndFailedPaint(t *testing.T) {
	for _, condition := range []string{"fenced", "hidden", "write-failed"} {
		t.Run(condition, func(t *testing.T) {
			original := model.New(t.TempDir(), 120, 40)
			updated := model.New(t.TempDir(), 120, 40)
			updated.Apply(model.CmdNewWorkspace{})
			output, err := os.CreateTemp(t.TempDir(), "terminal-")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			m := &machineClient{session: "painted", active: "local", order: []string{"local", "remote"}, out: output, size: tui.Winsize{Cols: 120, Rows: 40}, settings: &localSettings{config: &config.Config{}}, endpoints: map[string]*managedEndpoint{
				"local":  {profile: machine.Profile{ID: "local", Session: "painted", Enabled: true}, snapshot: &render.Input{State: original}},
				"remote": {profile: machine.Profile{ID: "remote", Session: "other", Enabled: true}, snapshot: &render.Input{State: original}},
			}}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			if saved := m.reconnectSelection("local"); saved == nil || saved.Workspace != 1 {
				t.Fatalf("initial paint not saved: %+v", saved)
			}
			switch condition {
			case "fenced":
				m.outputFence.Written([]byte("\x1b_Ga=p,i=1;\x1b\\"), time.Now())
			case "hidden":
				m.active = "remote"
			case "write-failed":
				output.Close()
			}
			// The real receive path may paint only the active endpoint and may defer it.
			_, done := m.event(endpointEvent{id: "local", message: msg{Type: "snapshot", snapshot: &render.Input{State: updated}}})
			if condition == "write-failed" && !done {
				t.Fatal("closed terminal output was not rejected")
			}
			saved := m.reconnectSelection("local")
			if saved == nil || saved.Workspace != 1 {
				t.Fatalf("%s replaced painted selection with received workspace: %+v", condition, saved)
			}
			if m.endpoints["local"].snapshot.State.ActiveWorkspace().ID != 2 {
				t.Fatal("new snapshot was not received")
			}
		})
	}
}

func TestPaintedSelectionReconnectBeforeFencedSnapshot(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	e := m.endpoints["local"]
	m.outputFence.Accept([]byte("\x1b[0n"))
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	m.outputFence.Written([]byte("\x1b_Ga=p,i=1;\x1b\\"), time.Now())
	h.api("local", api.MethodWorkspaceFocus, map[string]any{"workspace_id": "s2"})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for e.snapshot.State.ActiveWorkspace().ID != 2 {
		select {
		case event := <-m.events:
			m.event(event)
		case <-deadline.C:
			t.Fatal("new workspace snapshot never arrived")
		}
	}
	if !m.outputFence.Waiting(time.Now()) {
		t.Fatal("snapshot was not received during the paint fence")
	}
	e.connection.Close()
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("local", stream)
	attempt := e.attempt
	waitSelection(t, m, "reconnected painted workspace", func() bool { return e.snapshotAttempt == attempt })
	if e.snapshot.State.ActiveWorkspace().ID != 1 {
		t.Fatalf("restored received, unpainted workspace s%d instead of painted s1", e.snapshot.State.ActiveWorkspace().ID)
	}
	t.Logf("received-but-unpainted workspace=s2; reconnected workspace=s%d pane=p%d", e.snapshot.State.ActiveWorkspace().ID, e.snapshot.State.FocusedPane())
}

func TestPaintedSelectionStaysWithinTargetSession(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	e := m.endpoints["remote"]
	m.active = "remote"
	h.api("remote", api.MethodWorkspaceFocus, map[string]any{"workspace_id": "s2"})
	waitSelection(t, m, "paint X workspace2", func() bool { return e.snapshot.State.ActiveWorkspace().ID == 2 })
	m.outputFence.Accept([]byte("\x1b[0n"))
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	// While Y is displayed, X receives a different canonical selection.
	m.active = "local"
	h.api("remote", api.MethodWorkspaceFocus, map[string]any{"workspace_id": "s1"})
	waitSelection(t, m, "hidden X receives workspace1", func() bool { return e.snapshot.State.ActiveWorkspace().ID == 1 })
	e.connection.Close()
	x, err := net.Dial("unix", api.ClientSocketPath(h.dirs["remote"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", x)
	attempt := e.attempt
	waitSelection(t, m, "same X restores painted workspace2", func() bool { return e.snapshotAttempt == attempt })
	if e.snapshot.State.ActiveWorkspace().ID != 2 {
		t.Fatal("same-target reconnect used hidden received selection instead of painted s2")
	}
	t.Log("same-session X reconnect restored painted s2 despite hidden received s1")
	// Y uses the same IDs, but has an existing viewer focused on p3 in s1.
	h.api("local", api.MethodPaneFocus, api.PaneTarget{PaneID: "p3"})
	waitSelection(t, m, "Y default pane3", func() bool { return m.endpoints["local"].snapshot.State.FocusedPane() == 3 })
	// Keep Y's canonical default at s1/p3, but its existing viewer on
	// s2/p4. Old X IDs s2/p2 must change neither selection nor shared focus.
	h.api("local", api.MethodPaneSplit, map[string]any{"target_pane_id": "p2", "direction": "right", "focus": true})
	if err := m.endpoints["local"].connection.enqueue(msg{Type: "workspace_select", Workspace: 2}); err != nil {
		t.Fatal(err)
	}
	waitSelection(t, m, "existing Y viewer on workspace2 pane4", func() bool {
		return m.endpoints["local"].snapshot.State.ActiveWorkspace().ID == 2 && m.endpoints["local"].snapshot.State.FocusedPane() == 4
	})
	m.active = "remote"
	m.outputFence.Accept([]byte("\x1b[0n"))
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	retained := e.snapshot
	e.connection.Close()
	teststate.Setenv(t, "IKESU_STATE_DIR", filepath.Dir(h.dirs["local"]))
	profile := e.profile
	profile.Session = "right-local"
	profile.Target = "changed-host"
	profile.Enabled = false // Apply catalog changes without starting an SSH transport.
	data, err := json.Marshal([]machine.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.catalogPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshMachineCatalog(); err != nil {
		t.Fatal(err)
	}
	if e.snapshot != retained {
		t.Fatal("display snapshot was discarded")
	}
	if e.selection != nil || m.reconnectSelection("remote") != nil {
		t.Fatal("old session selection retained after catalog target change")
	}
	// Repainting the retained X screen must not bind it to Y.
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if e.selection != nil {
		t.Fatal("retained old snapshot saved as a new target selection")
	}
	e.profile.Enabled = true
	y, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", y)
	attempt = e.attempt
	waitSelection(t, m, "new Y keeps its default workspace and pane", func() bool { return e.snapshotAttempt == attempt })
	if got := endpoint.CaptureSelection(e.snapshot.State); got.Workspace != 1 || got.Pane != 3 {
		t.Fatalf("X selection changed Y default: %+v", got)
	}
	// Force a new Y snapshot for its pre-existing viewer and check shared focus.
	label := "Y-existing-viewer"
	h.api("local", api.MethodPaneRename, api.PaneRenameParams{PaneID: "p4", Label: &label})
	waitSelection(t, m, "Y existing viewer still focused", func() bool { return m.endpoints["local"].snapshot.State.PaneLabels[4] == "Y-existing-viewer" })
	if m.endpoints["local"].snapshot.State.FocusedPane() != 4 {
		t.Fatal("target switch changed existing Y viewer's shared pane focus")
	}
	t.Log("X→Y: candidate Y kept s1/p3; existing Y viewer kept s2/p4; retained X snapshot not restored")
}

func waitSelection(t *testing.T, m *machineClient, label string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !ready() {
		select {
		case event := <-m.events:
			if _, done := m.event(event); done {
				t.Fatalf("%s ended terminal: %+v", label, event)
			}
			if err := m.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")}); err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatalf("timeout: %s", label)
		}
	}
}

func TestPaintedSelectionCatalogIdentity(t *testing.T) {
	teststate.Lock(t)
	for _, changed := range []string{"host", "session", "label"} {
		t.Run(changed, func(t *testing.T) {
			teststate.Setenv(t, "IKESU_STATE_DIR", t.TempDir())
			output, err := os.CreateTemp(t.TempDir(), "terminal-")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			state := model.New(t.TempDir(), 120, 40)
			state.Apply(model.CmdNewWorkspace{})
			e := &managedEndpoint{profile: machine.Profile{ID: "remote", Target: "host-X", Session: "X", Enabled: true}, snapshot: &render.Input{State: state}}
			m := &machineClient{session: "catalog-selection", active: "remote", order: []string{"local", "remote"}, out: output, size: tui.Winsize{Cols: 120, Rows: 40}, settings: &localSettings{config: &config.Config{}}, endpoints: map[string]*managedEndpoint{"local": {}, "remote": e}}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			saved := m.reconnectSelection("remote")
			if saved == nil {
				t.Fatal("paint did not save selection")
			}
			profile := e.profile
			profile.Enabled = false
			switch changed {
			case "host":
				profile.Target = "host-Y"
			case "session":
				profile.Session = "Y"
			case "label":
				profile.Label = "renamed"
			}
			data, err := json.Marshal([]machine.Profile{profile})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(m.catalogPath()), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.catalogPath(), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := m.refreshMachineCatalog(); err != nil {
				t.Fatal(err)
			}
			if e.snapshot == nil {
				t.Fatal("active endpoint lost retained display")
			}
			got := m.reconnectSelection("remote")
			if changed == "label" {
				if got != saved {
					t.Fatal("unchanged target lost selection")
				}
			} else if got != nil {
				t.Fatal("host/session change retained old selection")
			}
		})
	}
}
