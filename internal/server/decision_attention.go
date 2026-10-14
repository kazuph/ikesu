package server

import "github.com/kazuph/goterrace/internal/api"

func (s *Server) completeDecisionOrigin(origin *api.DecisionOrigin) {
	if origin == nil {
		return
	}
	pid, wi, ti, ok := s.findPane(origin.PaneID)
	if !ok {
		return
	}
	if origin.WorkspaceLabel == "" {
		origin.WorkspaceLabel = s.wsInfo(wi).Label
	}
	if origin.TabLabel == "" {
		origin.TabLabel = s.tabInfo(wi, ti).Label
	}
	if origin.PaneLabel == "" {
		origin.PaneLabel = s.app.State().PaneLabels[pid]
		if origin.PaneLabel == "" {
			if p := s.sup.Get(pid); p != nil {
				origin.PaneLabel = p.Snapshot().OSCTitle
			}
		}
	}
}

func (s *Server) syncDecisionAttention() {
	if s.store == nil {
		return
	}
	pending, err := s.store.Decisions("pending")
	if err != nil {
		s.app.SetConfigWarning("decision attention: " + err.Error())
		return
	}
	panes := map[int]bool{}
	for _, d := range pending {
		if d.Origin == nil {
			continue
		}
		if pid, _, _, ok := s.findPane(d.Origin.PaneID); ok {
			panes[pid] = true
		}
	}
	s.app.SetDecisionAttention(panes)
}
