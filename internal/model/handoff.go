package model

// HandoffCounters preserves runtime-only pane IDs as well as durable IDs.
type HandoffCounters struct{ Pane, RuntimePane, Workspace, Tab int }

func (s *AppState) HandoffCounters() HandoffCounters {
	return HandoffCounters{s.nextPaneID, s.nextRuntimePaneID, s.nextWorkspaceID, s.nextTabID}
}
func (s *AppState) RestoreHandoffCounters(c HandoffCounters) {
	s.nextPaneID, s.nextRuntimePaneID, s.nextWorkspaceID, s.nextTabID = c.Pane, c.RuntimePane, c.Workspace, c.Tab
}
