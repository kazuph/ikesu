package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestMachineApprovalStateAndWholeURLWrapWithoutOverwritingPane(t *testing.T) {
	for _, status := range []string{"awaiting approval", "承認待ち。クリックで承認ページを開く"} {
		in := Input{State: testState(160, 45)}
		url := "https://example.invalid/check/token-with-a-full-query?confirm=one&machine=remote"
		machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "Local", Status: "online", Input: &in}, {ID: "remote", Label: "Mini test", Status: status, Reason: url}}, Active: "local"}
		out := ComposeMachines(in, &machines)
		area := SidebarViewForInput(in).Agents.Rect
		var text strings.Builder
		for y := area.Y; y < area.Y+area.H; y++ {
			var row strings.Builder
			for x := area.X; x < area.X+area.W; x++ {
				row.WriteString(out.Frame.Cell(x, y).Content)
			}
			text.WriteString(strings.TrimRight(row.String(), " "))
		}
		if !strings.Contains(text.String(), status) || !strings.Contains(text.String(), url) {
			t.Fatalf("approval state/URL lost: %q", text.String())
		}
		found := false
		for _, hit := range machines.Hits {
			if hit.Machine == "remote" && hit.Header {
				found = true
			}
		}
		if !found {
			t.Fatal("approval retry header has no input target")
		}
		plain := Compose(in)
		pane := in.State.PanesArea()
		for y := pane.Y; y < pane.Y+pane.H; y++ {
			for x := pane.X; x < pane.X+pane.W; x++ {
				if !reflect.DeepEqual(out.Frame.Cell(x, y), plain.Frame.Cell(x, y)) {
					t.Fatal("approval state overwrote active Local pane", x, y)
				}
			}
		}
	}
}

func TestMachineApprovalRowsStayAtConfiguredWidthWhileDragging(t *testing.T) {
	in := Input{State: testState(240, 60)}
	for _, normal := range []int{model.SidebarMinDefault, model.SidebarNormalDefault, model.SidebarMaxDefault} {
		in.SidebarConfig.UI.RightSidebarWidth = &normal
		m := MachineSidebar{Active: "local", Groups: []MachineGroup{
			{ID: "remote", Status: "awaiting approval", Reason: "https://example.invalid/check/token-with-a-full-query?confirm=one&machine=remote"},
			{ID: "local", Input: &in},
		}}
		in.State.RightSidebarSize = normal
		want := m.rightRows(in)
		for width := model.SidebarMaxDefault; width >= model.SidebarCollapsedWidth; width-- {
			in.State.RightSidebarSize = width
			if got := m.rightRows(in); !reflect.DeepEqual(got, want) {
				t.Fatalf("configured=%d actual=%d changed approval text/row count/targets", normal, width)
			}
		}
	}
}

func TestMachineSidebarKeepsOriginalIDsAndOnlySelectedEndpointAccent(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	for i, state := range []*Input{{State: localState}, {State: remoteState}} {
		state.State.Workspaces[0].ID = 1
		state.State.Workspaces[0].Name = []string{"Local real", "Remote real"}[i]
		state.State.Workspaces[0].CustomName = true
	}
	local := Input{ServerUI: true, State: localState}
	remote := Input{ServerUI: true, State: remoteState}
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "Local", Status: "online", Input: &local}, {ID: "m-original", Label: "Mini test", Status: "online", Input: &remote}}, Active: "local"}
	for _, selected := range []string{"local", "m-original"} {
		machines.Active = selected
		active := local
		if selected != "local" {
			active = remote
		}
		out := ComposeMachines(active, &machines)
		text := screenText(t, out, 160, 45)
		for _, want := range []string{"Local real", "Remote real", "▾ Mini test", "Local: online", "Mini test: online"} {
			if !strings.Contains(text, want) {
				t.Fatal("missing machine/sidebar content", want)
			}
		}
		found := map[string]MachineHit{}
		for _, hit := range machines.Hits {
			if hit.Row.Kind == RowWorkspace && hit.Workspace != 0 {
				found[hit.Machine] = hit
			}
		}
		if len(found) != 2 {
			t.Fatal("machine workspace hit targets missing", found)
		}
		for machine, hit := range found {
			if hit.Workspace != 1 {
				t.Fatal("machine workspace ID was rewritten", hit)
			}
			cell := out.Frame.Cell(hit.Rect.X, hit.Rect.Y)
			if (cell.Content == "▌") != (machine == selected) {
				t.Fatal("accent leaked across endpoints", machine, selected, cell)
			}
		}
	}
	if localState.Active != 0 || remoteState.Active != 0 {
		t.Fatal("composition changed endpoint navigation state")
	}
	plain := Compose(local)
	without := ComposeMachines(local, nil)
	if !reflect.DeepEqual(plain, without) {
		t.Fatal("no-machine composition changed")
	}
}

func TestMachineSidebarScrollRetainsHeadingsAndFullCardWidth(t *testing.T) {
	local, remote, second := Input{ServerUI: true, State: testState(160, 23)}, Input{ServerUI: true, State: testState(160, 23)}, Input{ServerUI: true, State: testState(160, 23)}
	for i := 0; i < 5; i++ {
		remote.State.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Label: "Mini test", Input: &remote}, {ID: "second", Label: "Second mini", Input: &second}}}
	for _, scenario := range []struct{ offset, remoteHeader, firstCard, secondHeader int }{{0, 8, 0, -1}, {3, 5, 0, -1}, {12, 5, 3, 17}} {
		m.Scroll = scenario.offset
		out := ComposeMachines(local, &m)
		if out.Frame.Cell(0, 2).Content != "▾" {
			t.Fatal("Local section heading scrolled out")
		}
		found := false
		for _, hit := range m.Hits {
			area := SidebarViewForInput(local).Workspace.Rect
			if hit.Rect.X != area.X || hit.Rect.Y >= area.Y+area.H {
				continue
			}
			if !hit.New && hit.Rect.W != 25 {
				t.Fatal("scrollbar narrowed machine hit area", hit)
			}
			if hit.Header && hit.Machine == "remote" && hit.Rect.Y != scenario.remoteHeader {
				t.Fatal("machine heading moved with cards", hit, scenario)
			}
			if hit.Workspace != 0 && hit.Row.Kind == RowWorkspace && hit.Machine == "remote" && !found {
				if hit.Row.Ws != scenario.firstCard {
					t.Fatal("wrong first visible card", hit, scenario)
				}
				found = true
			}
			if hit.Header && hit.Machine == "second" && hit.Rect.Y != scenario.secondHeader {
				t.Fatal("next group clipped or misplaced", hit, scenario)
			}
		}
		if !found {
			t.Fatal("remote cards disappeared")
		}
		if scenario.secondHeader >= 0 {
			if out.Frame.Cell(0, scenario.secondHeader).Content != "▾" {
				t.Fatal("second machine heading missing")
			}
		}
	}
}

func TestMachineSidebarDropIndicatorUsesRemoteGroupCoordinates(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	remoteState.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	local, remote := Input{ServerUI: true, State: localState}, Input{ServerUI: true, State: remoteState}
	drop := 2
	remote.Dragging, remote.DraggedWorkspace, remote.DropIndex = true, 0, &drop
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}, Active: "remote"}
	out := ComposeMachines(remote, &machines)
	last := -1
	for _, hit := range machines.Hits {
		if hit.Machine == "remote" && hit.Row.Ws == 1 && (hit.Row.Kind == RowWorkspace || hit.Row.Kind == RowGit || hit.Row.Kind == RowCardBlank) {
			last = max(last, hit.Rect.Y)
		}
	}
	if last < 0 {
		t.Fatal("remote card hit area missing")
	}
	area := SidebarViewForInput(remote).Workspace.Rect
	for x := area.X; x < area.X+area.W; x++ {
		cell := out.Frame.Cell(x, last+1)
		if cell.Content != "─" || !reflect.DeepEqual(cell.Style.Fg, out.Frame.Palette.Accent) {
			t.Fatal("remote insertion line missing", x, last+1, cell)
		}
	}
	machines.Active = "local"
	out = ComposeMachines(local, &machines)
	if out.Frame.Cell(area.X, last+1).Content == "─" {
		t.Fatal("hidden remote drag leaked into Local")
	}
	machines.DropMachine, machines.DropIndex = "remote", &drop
	out = ComposeMachines(local, &machines)
	if out.Frame.Cell(area.X, last+1).Content != "─" {
		t.Fatal("pending remote drop indicator was not drawn while Local remains selected")
	}
}

func TestPendingMachineSectionHighlightsOnlyItsTargetWithoutSelectingRemote(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	remoteState.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true, Section: model.SectionWork})
	local, remote := Input{ServerUI: true, State: localState}, Input{ServerUI: true, State: remoteState}
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}, Active: "local", DropMachine: "remote", DropSection: model.SectionWork}
	out := ComposeMachines(local, &machines)
	found := false
	for _, hit := range machines.Hits {
		if hit.Machine == "remote" && hit.Row.Kind == RowSection && hit.Row.Section == model.SectionWork {
			found = true
			for x := hit.Rect.X; x < hit.Rect.X+hit.Rect.W; x++ {
				cell := out.Frame.Cell(x, hit.Rect.Y)
				if cell.Width == 0 {
					previous := out.Frame.Cell(x-1, hit.Rect.Y)
					if previous == nil || previous.Width != 2 {
						t.Fatal("section header has an orphaned wide-character continuation", x)
					}
					cell = previous
				}
				if !reflect.DeepEqual(cell.Style.Bg, out.Frame.Palette.Accent) {
					t.Fatal("pending section target is not highlighted across its header", x)
				}
			}
		}
		if hit.Machine == "remote" && hit.Workspace != 0 && out.Frame.Cell(hit.Rect.X, hit.Rect.Y).Content == "▌" {
			t.Fatal("pending section move selected a hidden Remote card")
		}
		if hit.Machine == "remote" && hit.Row.Kind == RowWorkspace && remoteState.Workspaces[hit.Row.Ws].SidebarSection() == model.SectionWork {
			for x := hit.Rect.X; x < hit.Rect.X+hit.Rect.W; x++ {
				cell := out.Frame.Cell(x, hit.Rect.Y)
				if cell.Content != "─" || !reflect.DeepEqual(cell.Style.Fg, out.Frame.Palette.Accent) {
					t.Fatal("pending section move has no insertion line at its target", x)
				}
			}
		}
	}
	if !found || remote.DragSection != "" || localState.Active != 0 || remoteState.Active != 0 {
		t.Fatal("missing target or composition mutated endpoint navigation")
	}
}

func TestMachineInsertionBeforeCardUsesPrecedingBlankRow(t *testing.T) {
	local, remote := Input{ServerUI: true, State: testState(160, 45)}, Input{ServerUI: true, State: testState(160, 45)}
	remote.State.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	remote.State.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	drop := 1
	m := MachineSidebar{Active: "remote", Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}, DropMachine: "remote", DropIndex: &drop}
	out := ComposeMachines(remote, &m)
	for _, hit := range m.Hits {
		if hit.Machine == "remote" && hit.Workspace != 0 && hit.Row.Kind == RowWorkspace && hit.Row.Ws == drop {
			for x := 0; x < hit.Rect.W; x++ {
				c := out.Frame.Cell(x, hit.Rect.Y-1)
				if c.Content != "─" || !reflect.DeepEqual(c.Style.Fg, out.Frame.Palette.Accent) {
					t.Fatal("insertion line is not on preceding card gap", x, hit, c)
				}
			}
			if out.Frame.Cell(0, hit.Rect.Y).Content == "─" {
				t.Fatal("insertion line erased target workspace name")
			}
			return
		}
	}
	t.Fatal("target workspace missing")
}
