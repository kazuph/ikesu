package model

import (
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/session"
)

// Snapshot renders the state as a session.File for persistence. Pane
// metadata beyond the label (cwd, agent record) lives outside the model;
// the server fills it in via session.File.EachPane before saving.
func (s *AppState) Snapshot() *session.File {
	f := &session.File{
		Active:       s.Active,
		SidebarWidth: s.ExpandedSidebarWidth(), SidebarManual: s.SidebarManual,
		CollapsedSections: map[string]bool{},
		SidebarVisible:    s.SidebarVisible,
		NextPaneID:        s.nextPaneID,
		NextWorkspaceID:   s.nextWorkspaceID,
		NextTabID:         s.nextTabID,
	}
	for k, v := range s.CollapsedSections {
		f.CollapsedSections[string(k)] = v
	}
	for _, ws := range s.Workspaces {
		sw := session.Workspace{
			Worktree: ws.Worktree, GroupCollapsed: ws.GroupCollapsed, Section: string(ws.Section), CustomName: ws.CustomName, DefaultName: ws.DefaultName,
			ID:        ws.ID,
			Name:      ws.Name,
			Cwd:       ws.Cwd,
			ActiveTab: ws.ActiveTab,
		}
		for _, t := range ws.Tabs {
			st := session.Tab{
				ID:      t.ID,
				Zoomed:  t.Zoomed,
				Name:    t.Name,
				Focused: t.Layout.Focused(),
				Order:   t.Layout.PaneIDs(),
				Layout:  session.FromLayoutNode(t.Layout.Root()),
			}
			for _, pid := range st.Order {
				st.Panes = append(st.Panes, session.Pane{
					ID:    pid,
					Cwd:   ws.Cwd, // caller overwrites with the pane's real cwd
					Label: s.PaneLabels[pid],
				})
			}
			sw.Tabs = append(sw.Tabs, st)
		}
		f.Workspaces = append(f.Workspaces, sw)
	}
	return f
}

// Restore builds an AppState from a session file that has already passed
// session.File.Validate. Restored panes keep their saved ids and the
// allocation counters advance beyond every id in use (SPEC G5). Runtime
// pane processes are not spawned here; the caller walks the layouts and
// spawns a shell per leaf in the persisted cwd.
func Restore(f *session.File) *AppState {
	s := &AppState{
		Active:      f.Active,
		SidebarSize: f.SidebarWidth, SidebarManual: f.SidebarManual, CollapsedSections: map[Section]bool{},
		SidebarVisible:  f.SidebarVisible,
		PaneLabels:      map[PaneID]string{},
		nextPaneID:      f.NextPaneID,
		nextWorkspaceID: f.NextWorkspaceID,
		nextTabID:       f.NextTabID,
	}
	for k, v := range f.CollapsedSections {
		s.CollapsedSections[Section(k)] = v
	}
	for _, sw := range f.Workspaces {
		ws := &Workspace{ID: sw.ID, Name: sw.Name, Cwd: sw.Cwd, Worktree: sw.Worktree, GroupCollapsed: sw.GroupCollapsed, Section: Section(sw.Section), CustomName: sw.CustomName, DefaultName: sw.DefaultName}
		for _, st := range sw.Tabs {
			l := layout.Restore(session.ToLayoutNode(st.Layout), st.Focused, st.Order)
			ws.Tabs = append(ws.Tabs, &Tab{ID: st.ID, Name: st.Name, Layout: l, Zoomed: st.Zoomed})
			for _, p := range st.Panes {
				if p.Label != "" {
					s.PaneLabels[p.ID] = p.Label
				}
				if p.ID > s.nextPaneID {
					s.nextPaneID = p.ID
				}
			}
			if st.ID > s.nextTabID {
				s.nextTabID = st.ID
			}
		}
		ws.ActiveTab = sw.ActiveTab
		if ws.ActiveTab < 0 || ws.ActiveTab >= len(ws.Tabs) {
			ws.ActiveTab = 0
		}
		if ws.ID > s.nextWorkspaceID {
			s.nextWorkspaceID = ws.ID
		}
		s.Workspaces = append(s.Workspaces, ws)
	}
	if s.Active < 0 || s.Active >= len(s.Workspaces) {
		s.Active = 0
	}
	if s.PaneLabels == nil || len(s.PaneLabels) == 0 {
		s.PaneLabels = map[PaneID]string{}
	}
	return s
}
