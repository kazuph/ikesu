package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineDisplayOrderAndDrag(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IKESU_STATE_DIR", root)
	configPath := filepath.Join(root, "config.toml")
	requestPath := filepath.Join(root, "request.json")
	ssh := `#!/bin/sh
IFS= read -r request || exit 1
printf '%s\n' "$request" > "$IKESU_TEST_DRAG_REQUEST"
printf '%s\n' '{"id":"sidebar-move","result":{}}'
`
	if err := os.WriteFile(filepath.Join(root, "ssh"), []byte(ssh), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[remote]\nmanage_ssh_config = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("IKESU_TEST_DRAG_REQUEST", requestPath)
	t.Setenv(config.EnvVar, configPath)
	out, err := os.CreateTemp(root, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	m := cycleFixture(t)
	m.session, m.out = "display-order", out
	m.events = make(chan endpointEvent, 4)
	m.size = tui.Winsize{Cols: 160, Rows: 60}
	mini := m.endpoints["mini"]
	local := m.endpoints["local"]
	mini.snapshot.State.Active = 0
	local.profile.Label = "Local"
	local.snapshot.State.Cols, local.snapshot.State.Rows = m.size.Cols, m.size.Rows
	mini.snapshot.State.Cols, mini.snapshot.State.Rows = m.size.Cols, m.size.Rows
	var profiles []machine.Profile
	savedOrder := []string{"local"}
	for _, entry := range []struct {
		id, status string
		connected  bool
	}{
		{"cut-z", "disconnected", false},
		{"trying", "connecting", false},
		{"approval", "awaiting approval", true}, // A separate SSH action can await approval during a viewer session.
		{"ready-z", "online", true},
		{"retry", "reconnecting", false},
		{"cut-a", "disconnected", false},
		{"mini", "online", true},
		{"attention", "needs attention", false},
		{"stale", "online", false},
	} {
		profile := machine.Profile{ID: entry.id, Label: entry.id, Target: entry.id, Session: "default", Enabled: entry.status != "disconnected"}
		e := m.endpoints[entry.id]
		if e == nil {
			e = &managedEndpoint{}
			if entry.connected {
				e.connection = &endpointConnection{writer: mini.connection.writer}
				e.snapshot = local.snapshot
			}
			m.endpoints[entry.id] = e
		}
		e.profile, e.status = profile, entry.status
		profiles = append(profiles, profile)
		savedOrder = append(savedOrder, entry.id)
	}
	if err := os.MkdirAll(filepath.Dir(m.catalogPath()), 0700); err != nil {
		t.Fatal(err)
	}
	saved, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.catalogPath(), saved, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.syncMachineCatalog(); err != nil {
		t.Fatal(err)
	}
	assertOrder := func(want, wantCycle []string) {
		t.Helper()
		screen := emulatedScreen(t, m, out.Name())
		var left, right, cycle []string
		for _, hit := range m.sidebar.Hits {
			if !hit.Header {
				continue
			}
			var row strings.Builder
			for x := hit.Rect.X; x < hit.Rect.X+hit.Rect.W; x++ {
				row.WriteString(screen.CellAt(x, hit.Rect.Y).Content)
			}
			if label := m.endpoints[hit.Machine].profile.Label; !strings.HasPrefix(row.String(), "▾ "+label) {
				t.Fatalf("header hit for %s points to %q", hit.Machine, row.String())
			}
			if hit.Right {
				right = append(right, hit.Machine)
			} else {
				left = append(left, hit.Machine)
			}
		}
		for _, target := range m.workspaceCycleTargets() {
			if len(cycle) == 0 || cycle[len(cycle)-1] != target.machine {
				cycle = append(cycle, target.machine)
			}
		}
		if !slices.Equal(left, want[1:]) || !slices.Equal(right, want) || !slices.Equal(cycle, wantCycle) {
			t.Fatalf("left %v, right %v, keys %v; want %v, %v, %v", left, right, cycle, want[1:], want, wantCycle)
		}
	}
	assertOrder([]string{"local", "ready-z", "mini", "trying", "approval", "retry", "attention", "stale", "cut-z", "cut-a"}, []string{"local", "ready-z", "mini", "approval"})
	m.active = "mini"
	workspaceHit := func(index int) render.MachineHit {
		t.Helper()
		for _, hit := range m.sidebar.Hits {
			if !hit.Right && hit.Machine == "mini" && hit.Workspace != 0 && hit.Row.Kind == render.RowWorkspace && hit.Row.Ws == index {
				return hit
			}
		}
		t.Fatalf("mini workspace %d is missing", index)
		return render.MachineHit{}
	}
	mouse := func(hit render.MachineHit, code int, suffix string) {
		t.Helper()
		raw := fmt.Sprintf("\x1b[<%d;%d;%d%s", code, hit.Rect.X+3, hit.Rect.Y+1, suffix)
		if err := m.input(mouseEvent(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	source := workspaceHit(0)
	mouse(source, 0, "M")
	if m.drag == nil || m.drag.workspace != source.Workspace {
		t.Fatalf("press on %+v did not select the drag source: %+v", source, m.drag)
	}
	mouse(workspaceHit(1), 32, "M")
	if m.drag == nil || !m.drag.started {
		t.Fatal("workspace drag did not start")
	}
	m.event(endpointEvent{id: "ready-z", err: errors.New("connection lost")})
	assertOrder([]string{"local", "mini", "trying", "approval", "ready-z", "retry", "attention", "stale", "cut-z", "cut-a"}, []string{"local", "mini", "approval"})
	drop := workspaceHit(1)
	if drop.Rect.Y >= source.Rect.Y {
		t.Fatal("target should have moved above the original drag start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mini.ctx = ctx
	mouse(drop, 0, "m")
	m.workers.Wait()
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request api.Request
	var move struct {
		Workspace string `json:"workspace_id"`
		Index     int    `json:"insert_index"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if err := request.DecodeParams(&move); err != nil {
		t.Fatal(err)
	}
	if request.Method != "workspace.move" || move.Workspace != api.FormatWorkspaceID(source.Workspace) || move.Index != 1 {
		t.Fatalf("unexpected workspace move: %s", data)
	}
	for _, hit := range m.sidebar.Hits {
		if !hit.Right && hit.Header && hit.Machine == "mini" {
			mouse(hit, 0, "M")
			mouse(hit, 0, "m")
			break
		}
	}
	if !m.sidebar.Collapsed["mini"] {
		t.Fatal("click on the moved header did not fold mini")
	}
	for _, hit := range m.sidebar.Hits {
		if !hit.Right && hit.Machine == "mini" && hit.Workspace != 0 {
			t.Fatalf("folded workspace kept a hit: %+v", hit)
		}
	}
	if got, err := os.ReadFile(m.catalogPath()); err != nil || !bytes.Equal(got, saved) || !slices.Equal(m.order, savedOrder) {
		t.Fatalf("saved machine order changed: %v, %s, %v", m.order, got, err)
	}
}
