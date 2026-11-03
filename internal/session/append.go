package session

import "crypto/rand"

// Append adds a validated import without changing existing IDs. Imported
// panes use the smallest unreserved IDs, or the high-water mark on DB failure.
func Append(target, imported *File, reserved map[int]bool, dbUnavailable bool) (*File, *File) {
	if target == nil {
		target = &File{Version: CurrentVersion, SidebarVisible: true}
	}
	out := *target
	used := map[int]bool{}
	for id := range reserved {
		used[id] = true
	}
	target.EachPane(func(_ *Workspace, _ *Tab, pane *Pane) { used[pane.ID] = true })
	out.Workspaces = append([]Workspace(nil), target.Workspaces...)
	added := *imported
	added.Workspaces = nil
	for _, w := range imported.Workspaces {
		out.NextWorkspaceID++
		w.ID = out.NextWorkspaceID
		w.Tabs = append([]Tab(nil), w.Tabs...)
		for ti := range w.Tabs {
			t := &w.Tabs[ti]
			out.NextTabID++
			t.ID = out.NextTabID
			ids := map[int]int{}
			t.Panes = append([]Pane(nil), t.Panes...)
			for pi := range t.Panes {
				p := &t.Panes[pi]
				id := 1
				if dbUnavailable {
					id = out.NextPaneID + 1
				} else {
					for used[id] {
						id++
					}
				}
				used[id] = true
				out.NextPaneID = max(out.NextPaneID, id)
				ids[p.ID] = id
				p.ID = id
				p.Generation = rand.Text()
			}
			t.Focused = ids[t.Focused]
			if t.Zoomed != 0 {
				t.Zoomed = ids[t.Zoomed]
			}
			order := make([]int, len(t.Order))
			for i, id := range t.Order {
				order[i] = ids[id]
			}
			t.Order = order
			var clone func(*Node) *Node
			clone = func(n *Node) *Node {
				c := *n
				if n.Leaf >= 0 {
					c.Leaf = ids[n.Leaf]
				} else {
					c.First = clone(n.First)
					c.Second = clone(n.Second)
				}
				return &c
			}
			t.Layout = clone(t.Layout)
		}
		added.Workspaces = append(added.Workspaces, w)
	}
	out.Active = len(out.Workspaces) + imported.Active
	out.SidebarWidth = imported.SidebarWidth
	out.SidebarManual = imported.SidebarManual
	out.SidebarVisible = imported.SidebarVisible
	out.CollapsedSections = map[string]bool{}
	for k, v := range target.CollapsedSections {
		out.CollapsedSections[k] = v
	}
	for k, v := range imported.CollapsedSections {
		out.CollapsedSections[k] = v
	}

	out.Workspaces = append(out.Workspaces, added.Workspaces...)
	added.NextPaneID = out.NextPaneID
	added.NextWorkspaceID = out.NextWorkspaceID
	added.NextTabID = out.NextTabID
	return &out, &added
}
