package client

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
)

func TestOnlineMachineNewButtonRequestsWorkspace(t *testing.T) {
	t.Parallel()
	m := searchFixture(t)
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	m.out = out
	remote := m.endpoints["mini"]
	remote.status = "online"
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range m.sidebar.Hits {
		if hit.Machine != "mini" || !hit.IsHeaderButton() {
			continue
		}
		if !hit.New {
			t.Fatalf("online machine has wrong button: %+v", hit)
		}
		click := fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+hit.Rect.W, hit.Rect.Y+1)
		if err := m.input(mouseEvent(t, click)); err != nil {
			t.Fatal(err)
		}
		q := remote.connection.queue
		if len(q) != 2 || q[0].Type != "resize" || q[1].Type != "workspace_new" {
			t.Fatalf("new button did not request a remote workspace: %+v", q)
		}
		// A queued click can still see the old hit before the next paint.
		m.pending = nil
		remote.connection = nil
		remote.status = "reconnecting"
		if err := m.input(mouseEvent(t, click)); err != nil {
			t.Fatal(err)
		}
		for _, updated := range m.sidebar.Hits {
			if updated.Machine == "mini" && updated.Disconnect {
				return
			}
		}
		t.Fatal("connection loss did not redraw the machine's disconnect button")
	}
	t.Fatal("online machine did not draw its new workspace button")
}

func TestMachineWorkspaceNewRealEndpoints(t *testing.T) {
	h := newRightMachineHarness(t)
	local := h.m.endpoints["local"]
	localCount := len(local.snapshot.State.Workspaces)
	if err := h.m.createWorkspace("local"); err != nil {
		t.Fatal(err)
	}
	h.until("local workspace", func() bool { return len(local.snapshot.State.Workspaces) == localCount+1 })
	e := h.m.endpoints["remote"]
	if !endpoint.Allows(e.connection.writer.available, endpoint.MachineGeneration, e.connection.writer.methods, "workspace_new") {
		t.Fatal("real endpoint did not advertise workspace_new")
	}
	before := len(e.snapshot.State.Workspaces)
	cwd := e.snapshot.State.ActiveWorkspace().Cwd
	if err := h.m.createWorkspace("remote"); err != nil {
		t.Fatal(err)
	}
	h.until("new workspace and switch", func() bool {
		return h.m.active == "remote" && h.m.pending == nil && len(e.snapshot.State.Workspaces) == before+1
	})
	if got := e.snapshot.State.ActiveWorkspace().Cwd; got != cwd {
		t.Fatalf("new workspace cwd = %q, want %q", got, cwd)
	}
}

func TestMachineWorkspaceNewLegacyCoordinatesAndNarrowGuard(t *testing.T) {
	m := searchFixture(t)
	m.inputBatch = true
	e := m.endpoints["local"]
	e.connection.writer.methods = []string{"input"}
	e.snapshot.State.SidebarVisible = true
	e.snapshot.State.Cols, e.snapshot.State.Rows = 120, 40
	if err := m.createWorkspace("local"); err != nil {
		t.Fatal(err)
	}
	if len(e.connection.queue) != 1 || e.connection.queue[0].Type != "input" {
		t.Fatalf("legacy input: %+v", e.connection.queue)
	}
	b, err := base64.StdEncoding.DecodeString(e.connection.queue[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	area := e.snapshot.State.SidebarArea()
	want := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", area.X+area.W-5, area.Y+area.H-2, area.X+area.W-5, area.Y+area.H-2)
	if string(b) != want {
		t.Fatalf("input %q, want %q", b, want)
	}
	e.connection.queue = nil
	e.snapshot.State.SidebarVisible = false
	if err := m.createWorkspace("local"); err != nil {
		t.Fatal(err)
	}
	if len(e.connection.queue) != 0 || m.notice == "" {
		t.Fatalf("collapsed sidebar sent input or omitted notice: %+v %q", e.connection.queue, m.notice)
	}
}
