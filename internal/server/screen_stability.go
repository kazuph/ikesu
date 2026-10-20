package server

import (
	"github.com/kazuph/ikesu/internal/screenprompt"
	"time"
)

func (s *Server) screenUserActive(pid int) bool {
	p := s.sup.Get(pid)
	return p != nil && p.UserActive()
}
func (s *Server) previewSeen(pid int, p *screenprompt.Prompt) bool {
	return s.screenPrompts.seen[pid][p.PreviewSignature()]
}
func (s *Server) markPreviewSeen(pid int, p *screenprompt.Prompt) {
	if s.screenPrompts.seen == nil {
		s.screenPrompts.seen = map[int]map[string]bool{}
	}
	if s.screenPrompts.seen[pid] == nil {
		s.screenPrompts.seen[pid] = map[string]bool{}
	}
	s.screenPrompts.seen[pid][p.PreviewSignature()] = true
}
func (s *Server) recentScreenRebuilds(pid int) []time.Time {
	var recent []time.Time
	for _, at := range s.screenPrompts.rebuilds[pid] {
		if time.Since(at) <= 30*time.Second {
			recent = append(recent, at)
		}
	}
	return recent
}
func (s *Server) screenRebuildFlood(pid int) bool { return len(s.recentScreenRebuilds(pid)) >= 3 }
func (s *Server) noteScreenRebuild(pid int) {
	if s.screenPrompts.rebuilds == nil {
		s.screenPrompts.rebuilds = map[int][]time.Time{}
	}
	s.screenPrompts.rebuilds[pid] = append(s.recentScreenRebuilds(pid), time.Now())
}
func (s *Server) updateScreenPrompt(w *screenPromptWatch) {
	if w.decisionID == "" {
		return
	}
	store, err := s.db()
	if err != nil {
		s.app.SetConfigWarning(err.Error())
		return
	}
	_, changed, err := store.UpdatePendingScreenDecision(w.decisionID, w.prompt.Params)
	if err != nil {
		s.app.SetConfigWarning("screen prompt refresh: " + err.Error())
		return
	}
	if changed {
		s.pushDecisionProjection()
	}
}
