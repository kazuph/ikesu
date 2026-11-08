package client

import (
	"encoding/json"

	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"

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
