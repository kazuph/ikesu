package client

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/render"
)

type collapseView struct {
	header     render.MachineHit
	newHit     bool
	glyph      string
	remoteRows []render.MachineHit
	localRows  map[int]string
	rows       map[int]string
}

func readCollapseView(t *testing.T, h *rightMachineHarness) collapseView {
	t.Helper()
	m := h.m
	frame := render.ComposeClient(*m.endpoints[m.active].snapshot, &m.sidebar, "test", false).Frame
	area := m.endpoints[m.active].snapshot.State.SidebarArea()
	text := func(y int) string {
		var b strings.Builder
		for x := area.X; x < area.X+area.W; x++ {
			if cell := frame.Cell(x, y); cell != nil {
				b.WriteString(cell.Content)
			}
		}
		return b.String()
	}
	view := collapseView{localRows: map[int]string{}, rows: map[int]string{}}
	found := false
	for _, hit := range m.sidebar.Hits {
		if hit.Right {
			continue
		}
		view.rows[hit.Rect.Y] = text(hit.Rect.Y)
		switch {
		case hit.Machine == "remote" && hit.Header:
			view.header, found = hit, true
			view.glyph = frame.Cell(hit.Rect.X, hit.Rect.Y).Content
		case hit.Machine == "remote" && hit.New:
			view.newHit = true
		case hit.Machine == "remote" && hit.Row.Kind != render.RowRule:
			view.remoteRows = append(view.remoteRows, hit)
		case hit.Machine == "local":
			view.localRows[hit.Rect.Y] = text(hit.Rect.Y)
		}
	}
	if !found {
		t.Fatal("remote machine header was not drawn")
	}
	return view
}

func clickCell(t *testing.T, h *rightMachineHarness, x, y int) {
	t.Helper()
	if err := h.m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))); err != nil {
		t.Fatal(err)
	}
	if err := h.m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))); err != nil {
		t.Fatal(err)
	}
}

// A machine header left click hides only that machine's sidebar rows and keeps
// the connection, server state and the active remote workspace unchanged.
func TestMachineHeaderClickCollapsesAndExpandsRealRemoteSpaces(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(0, 0, 160, 35)
	before := readCollapseView(t, h)
	if before.glyph != "▾" || !before.newHit || len(before.remoteRows) == 0 {
		t.Fatalf("expanded header glyph=%q new=%t rows=%d", before.glyph, before.newHit, len(before.remoteRows))
	}
	// Select a remote workspace so the collapsed machine owns the active space.
	var target render.MachineHit
	for _, hit := range before.remoteRows {
		if hit.Row.Kind == render.RowWorkspace && hit.Workspace != 0 {
			target = hit
			break
		}
	}
	if target.Workspace == 0 {
		t.Fatal("remote workspace row missing")
	}
	clickCell(t, h, target.Rect.X+2, target.Rect.Y)
	h.until("remote workspace selected", func() bool {
		return m.pending == nil && m.active == "remote" && m.endpoints["remote"].snapshot.State.ActiveWorkspace().ID == target.Workspace
	})
	m.drag = nil
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	selected := readCollapseView(t, h)
	remote := m.endpoints["remote"]
	connection, status, workspaces := remote.connection, remote.status, len(remote.snapshot.State.Workspaces)

	// Collapse by clicking the machine name.
	clickCell(t, h, selected.header.Rect.X+3, selected.header.Rect.Y)
	collapsed := readCollapseView(t, h)
	if collapsed.glyph != "▸" {
		t.Fatalf("collapsed header glyph=%q", collapsed.glyph)
	}
	if len(collapsed.remoteRows) != 0 {
		t.Fatalf("collapsed machine kept %d rows: %+v", len(collapsed.remoteRows), collapsed.remoteRows)
	}
	if !collapsed.newHit || collapsed.header.Rect.Y != selected.header.Rect.Y {
		t.Fatalf("header moved or lost [new]: %+v new=%t", collapsed.header, collapsed.newHit)
	}
	for y, row := range selected.localRows {
		if collapsed.localRows[y] != row {
			t.Fatalf("local row y=%d changed: %q -> %q", y, row, collapsed.localRows[y])
		}
	}
	// Every remaining click target must sit on the row it was drawn for.
	for y, row := range collapsed.rows {
		if strings.TrimSpace(row) == "" && y != collapsed.header.Rect.Y {
			continue
		}
		if y > collapsed.header.Rect.Y+1 {
			t.Fatalf("hit below collapsed header y=%d row=%q", y, row)
		}
	}
	if remote.connection != connection || remote.status != status || len(remote.snapshot.State.Workspaces) != workspaces {
		t.Fatal("collapse changed the remote connection or server state")
	}
	if m.active != "remote" || remote.snapshot.State.ActiveWorkspace().ID != target.Workspace {
		t.Fatal("collapse changed the selected remote workspace")
	}
	// Where a remote workspace row used to be is now inert.
	clickCell(t, h, target.Rect.X+2, target.Rect.Y)
	if m.pending != nil || m.drag != nil {
		t.Fatalf("hidden row still accepted a click: pending=%+v drag=%+v", m.pending, m.drag)
	}

	// Expand by clicking the ▾/▸ glyph itself.
	clickCell(t, h, collapsed.header.Rect.X, collapsed.header.Rect.Y)
	expanded := readCollapseView(t, h)
	if expanded.glyph != "▾" || len(expanded.remoteRows) != len(selected.remoteRows) {
		t.Fatalf("expanded glyph=%q rows=%d want %d", expanded.glyph, len(expanded.remoteRows), len(selected.remoteRows))
	}
	for i, hit := range expanded.remoteRows {
		if hit.Rect != selected.remoteRows[i].Rect || hit.Workspace != selected.remoteRows[i].Workspace {
			t.Fatalf("expanded hit %d moved: %+v want %+v", i, hit, selected.remoteRows[i])
		}
	}
	if remote.connection != connection || len(remote.snapshot.State.Workspaces) != workspaces {
		t.Fatal("expand changed the remote connection or server state")
	}
}

// A short viewport scrolled to the bottom clamps back when rows disappear so
// the drawn rows and click targets stay aligned.
func TestMachineHeaderCollapseClampsScrolledSidebar(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	for _, id := range m.order {
		for i := 0; i < 4; i++ {
			h.api(id, api.MethodWorkspaceCreate, map[string]any{"focus": false})
		}
	}
	h.until("extra workspaces", func() bool {
		for _, e := range m.endpoints {
			if len(e.snapshot.State.Workspaces) != 6 {
				return false
			}
		}
		return true
	})
	h.modes(0, 0, 160, 24)
	m.sidebar.Scroll = 1000
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	if m.sidebar.Scroll == 0 {
		t.Fatal("test viewport is not scrollable")
	}
	view := readCollapseView(t, h)
	clickCell(t, h, view.header.Rect.X+3, view.header.Rect.Y)
	collapsed := readCollapseView(t, h)
	if collapsed.glyph != "▸" || len(collapsed.remoteRows) != 0 {
		t.Fatalf("collapsed glyph=%q rows=%d", collapsed.glyph, len(collapsed.remoteRows))
	}
	for _, hit := range m.sidebar.Hits {
		if hit.Right || hit.Machine != "local" || hit.Workspace == 0 {
			continue
		}
		row := collapsed.rows[hit.Rect.Y]
		// Workspace cards show their 1-based number before the name.
		name := fmt.Sprintf(" %d ", hit.Row.Ws+1)
		if hit.Row.Kind == render.RowWorkspace && !strings.Contains(row, name) {
			t.Fatalf("local hit y=%d row=%q does not show %q", hit.Rect.Y, row, name)
		}
	}
}

// A one-column sidebar keeps the header target and draws its ▾/▸, so the
// clickable header stays visible and toggles like the wide one.
func TestMachineHeaderInOneColumnSidebarShowsToggle(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(1, 0, 160, 35)
	header := func() (render.MachineHit, string, int) {
		t.Helper()
		frame := render.ComposeClient(*m.endpoints[m.active].snapshot, &m.sidebar, "test", false).Frame
		var found *render.MachineHit
		rows := 0
		for i, hit := range m.sidebar.Hits {
			if hit.Right || hit.Machine != "remote" {
				continue
			}
			if hit.Header {
				found = &m.sidebar.Hits[i]
			} else if hit.Row.Kind != render.RowRule {
				rows++
			}
		}
		if found == nil {
			t.Fatal("one-column remote header target missing")
		}
		return *found, frame.Cell(found.Rect.X, found.Rect.Y).Content, rows
	}
	hit, glyph, rows := header()
	if hit.Rect.W != 1 || glyph != "▾" || rows == 0 {
		t.Fatalf("one-column header width=%d glyph=%q rows=%d", hit.Rect.W, glyph, rows)
	}
	clickCell(t, h, hit.Rect.X, hit.Rect.Y)
	collapsed, glyph, rows := header()
	if glyph != "▸" || rows != 0 || collapsed.Rect != hit.Rect {
		t.Fatalf("collapsed one-column header glyph=%q rows=%d rect=%+v", glyph, rows, collapsed.Rect)
	}
	clickCell(t, h, hit.Rect.X, hit.Rect.Y)
	if _, glyph, rows := header(); glyph != "▾" || rows == 0 {
		t.Fatalf("expanded one-column header glyph=%q rows=%d", glyph, rows)
	}
}
