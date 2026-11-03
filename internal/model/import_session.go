package model

import "github.com/kazuph/ikesu/internal/session"

// AppendSession adopts only the newly allocated workspaces, retaining
// runtime-only state (including overlays) and existing workspace objects.
func (s *AppState) AppendSession(merged, added *session.File) {
	restored := Restore(added)
	s.Active = merged.Active
	s.SidebarSize = merged.SidebarWidth
	s.SidebarManual = merged.SidebarManual
	s.SidebarVisible = merged.SidebarVisible
	s.CollapsedSections = map[Section]bool{}
	for k, v := range merged.CollapsedSections {
		s.CollapsedSections[Section(k)] = v
	}

	s.Workspaces = append(s.Workspaces, restored.Workspaces...)
	if s.PaneLabels == nil {
		s.PaneLabels = map[int]string{}
	}
	if s.PaneGenerations == nil {
		s.PaneGenerations = map[int]string{}
	}
	for id, generation := range restored.PaneGenerations {
		delete(s.PaneLabels, id)
		if label := restored.PaneLabels[id]; label != "" {
			s.PaneLabels[id] = label
		}
		s.PaneGenerations[id] = generation
		delete(s.LegacyPaneGenerations, id)
	}
	s.nextPaneID = merged.NextPaneID
	s.nextTabID = merged.NextTabID
	s.nextWorkspaceID = merged.NextWorkspaceID
}
