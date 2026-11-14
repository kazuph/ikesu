package render

import "testing"

// The strip's top cell shows » so it can be tapped; status cells stay as they were.
func TestCollapsedStripShowsExpandMark(t *testing.T) {
	s := testState(80, 24)
	s.SidebarVisible = false
	pid := s.Workspaces[0].Tabs[0].Layout.Focused()
	in := Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}}
	out := Compose(in)
	if got := out.Frame.Cell(0, 0).Content; got != "»" {
		t.Fatalf("top cell %q", got)
	}
	if got := out.Frame.Cell(0, 4).Content; got != "·" {
		t.Fatalf("status cell changed: %q", got)
	}
	machines := &MachineSidebar{Active: "a", Groups: []MachineGroup{{ID: "a", Label: "a", Input: &in}, {ID: "b", Label: "b", Input: &in}}}
	if got := ComposeClient(in, machines, "test", false).Frame.Cell(0, 0).Content; got != "»" {
		t.Fatalf("multi-machine top cell %q", got)
	}
	s.SidebarVisible = true
	if got := Compose(in).Frame.Cell(0, 0).Content; got == "»" {
		t.Fatal("expanded sidebar shows the strip mark")
	}
}
