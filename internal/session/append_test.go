package session

import "testing"

func TestAppendRekeysWithoutMutatingExistingSession(t *testing.T) {
	base := &File{Version: CurrentVersion, Active: 0, NextPaneID: 100, NextTabID: 12, NextWorkspaceID: 5, SidebarWidth: 30, Workspaces: []Workspace{{ID: 5, Name: "existing", Tabs: []Tab{{ID: 12, Focused: 100, Order: []int{100}, Layout: &Node{Leaf: 100}, Panes: []Pane{{ID: 100}}}}}}}
	src := &File{Active: 0, SidebarWidth: 27, Workspaces: []Workspace{{ID: 5, Name: "import", Tabs: []Tab{{ID: 12, Focused: 100, Order: []int{100}, Layout: &Node{Leaf: 100}, Panes: []Pane{{ID: 100, SessionID: "exact"}}}}}}}
	merged, added := Append(base, src)
	if len(merged.Workspaces) != 2 || merged.Workspaces[0].ID != 5 || merged.SidebarWidth != 27 || merged.Active != 1 {
		t.Fatalf("existing changed: %+v", merged)
	}
	w := added.Workspaces[0]
	tab := w.Tabs[0]
	if w.ID != 6 || tab.ID != 13 || tab.Focused != 101 || tab.Order[0] != 101 || tab.Layout.Leaf != 101 || tab.Panes[0].ID != 101 || merged.NextPaneID != 101 {
		t.Fatalf("ids not rekeyed: %+v %+v", w, tab)
	}
	if src.Workspaces[0].Tabs[0].Panes[0].ID != 100 || src.Workspaces[0].Tabs[0].Layout.Leaf != 100 || base.NextPaneID != 100 {
		t.Fatal("input mutated")
	}
}

func TestAppendRemapsZoom(t *testing.T) {
	target := &File{NextPaneID: 50, NextWorkspaceID: 20, NextTabID: 30}
	src := &File{Workspaces: []Workspace{{Tabs: []Tab{{Zoomed: 7, Focused: 7, Order: []int{7, 8}, Layout: &Node{Leaf: -1, Direction: "v", Ratio: .5, First: &Node{Leaf: 7}, Second: &Node{Leaf: 8}}, Panes: []Pane{{ID: 7}, {ID: 8}}}}}}}
	merged, added := Append(target, src)
	if added.Workspaces[0].Tabs[0].Zoomed != 51 || merged.Workspaces[0].Tabs[0].Zoomed != 51 {
		t.Fatal("zoom not remapped")
	}
}
