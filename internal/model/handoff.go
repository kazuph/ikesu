package model

import "crypto/rand"

// HandoffCounters preserves runtime-only pane IDs as well as durable IDs.
type HandoffCounters struct{ Pane, RuntimePane, Workspace, Tab int }

func (s *AppState) HandoffCounters() HandoffCounters {
	return HandoffCounters{s.nextPaneID, s.nextRuntimePaneID, s.nextWorkspaceID, s.nextTabID}
}
func (s *AppState) RestoreHandoffCounters(c HandoffCounters) {
	s.nextPaneID, s.nextRuntimePaneID, s.nextWorkspaceID, s.nextTabID = c.Pane, c.RuntimePane, c.Workspace, c.Tab
}

// EnsureWorkspaceGenerations gives every workspace an identity generation.
// Servers started before generations existed hand off workspaces with an
// empty value, and live handoff never runs Restore, so fill them here.
func (s *AppState) EnsureWorkspaceGenerations() {
	for _, ws := range s.Workspaces {
		if ws.Generation == "" {
			ws.Generation = rand.Text()
		}
	}
}
