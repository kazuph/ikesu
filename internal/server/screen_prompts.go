package server

import (
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/screenprompt"
)

type screenPromptHub struct {
	watches map[int]*screenPromptWatch
	timer   *time.Timer
}

type screenPromptWatch struct {
	signature   string
	decisionID  string
	prompt      *screenprompt.Prompt
	mismatches  int
	lastMatched bool
	resolved    bool
	preview     *screenPreviewScan
	previews    map[string]*string
	delivery    <-chan error
}

func (s *Server) readScreenPrompt(pid int) *screenprompt.Prompt {
	p := s.sup.Get(pid)
	if p == nil || p.Snapshot().Dead {
		return nil
	}
	agent := p.Snapshot().AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	return screenprompt.Parse(agent, p.DetectionLines())
}

func (s *Server) screenPromptsActive() bool {
	if !s.cfg.ScreenPrompts() {
		return false
	}
	if len(s.screenPrompts.watches) > 0 {
		return true
	}
	active := false
	s.eachPane(func(pid, _, _ int) {
		if s.app.PaneStatusKey(pid) != "blocked" {
			return
		}
		p := s.sup.Get(pid)
		if p == nil || p.Snapshot().Dead {
			return
		}
		agent := p.Snapshot().AgentID
		if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
			agent = reported
		}
		active = active || agent == "claude" || agent == "codex" || agent == "devin"
	})
	return active
}

func (s *Server) scheduleScreenPrompts() {
	if s.screenPrompts.timer != nil || !s.screenPromptsActive() {
		return
	}
	s.initDecisionHub()
	s.screenPrompts.timer = time.AfterFunc(eventWaitPoll, func() {
		select {
		case <-s.decisions.stopped:
			return
		default:
		}
		s.app.Do(func() {
			s.screenPrompts.timer = nil
			select {
			case <-s.decisions.stopped:
				return
			default:
			}
			s.scanScreenPrompts()
			s.scheduleScreenPrompts()
		})
	})
}

func (s *Server) stopScreenPromptTimer() {
	if s.screenPrompts.timer != nil {
		s.screenPrompts.timer.Stop()
		s.screenPrompts.timer = nil
	}
}

func (s *Server) screenPromptConfig(c config.Config) {
	s.cfg = c
	if c.ScreenPrompts() {
		s.scheduleScreenPrompts()
		return
	}
	s.stopScreenPromptTimer()
	for pid, w := range s.screenPrompts.watches {
		if w.preview != nil {
			s.restorePreviewCursor(pid, w)
		}
		s.cancelScreenPrompt(w)
	}
	s.screenPrompts.watches = nil
	// Include pending records restored before their pane's first scan.
	if s.store != nil {
		pending, err := s.store.Decisions("pending")
		if err != nil {
			s.app.SetConfigWarning(err.Error())
			return
		}
		for _, d := range pending {
			if d.Origin != nil && d.Origin.Reason == "screen_prompt" {
				w := &screenPromptWatch{decisionID: d.DecisionID}
				s.cancelScreenPrompt(w)
			}
		}
	}
}

func (s *Server) cancelScreenPrompt(w *screenPromptWatch) {
	if w.decisionID == "" || w.resolved {
		return
	}
	store, err := s.db()
	if err == nil {
		var d api.Decision
		d, err = store.CancelDecision(w.decisionID, time.Now())
		if err == nil {
			s.decisionChanged("decision_resolved", d)
			w.resolved = true
			return
		}
		if d.Status != "pending" {
			w.resolved = true
			return
		}
	}
	s.app.SetConfigWarning("screen prompt cancellation: " + err.Error())
}

func (s *Server) scanScreenPrompts() {
	if !s.cfg.ScreenPrompts() {
		return
	}
	if s.screenPrompts.watches == nil {
		s.screenPrompts.watches = map[int]*screenPromptWatch{}
	}
	panes := map[int]bool{}
	s.eachPane(func(pid, _, _ int) { panes[pid] = true })
	for pid := range s.screenPrompts.watches {
		panes[pid] = true
	}
	for pid := range panes {
		w := s.screenPrompts.watches[pid]
		if w == nil && s.app.PaneStatusKey(pid) != "blocked" {
			continue
		}
		p := s.readScreenPrompt(pid)
		if w != nil {
			if p == nil || p.Signature() != w.signature {
				w.lastMatched = false
				w.mismatches++
				if w.preview != nil {
					s.restorePreviewCursor(pid, w)
					s.cachePreviewScan(w)
				}
				if w.mismatches < 2 {
					continue
				} // SPEC 工程6: one damaged read cannot cancel.
				s.cancelScreenPrompt(w)
				delete(s.screenPrompts.watches, pid)
				w = nil
			} else {
				w.lastMatched = true
				w.mismatches = 0
				w.prompt = p
				s.applyCapturedPreviews(w)
				if w.preview != nil {
					s.stepPreviewScan(pid, w, p)
					continue
				}
				if w.decisionID == "" {
					s.createScreenPrompt(pid, w)
					continue
				}
				if !w.resolved {
					store, err := s.db()
					if err == nil {
						if d, err := store.Decision(w.decisionID); err == nil {
							w.resolved = d.Status != "pending"
						}
					}
				}
				continue
			}
		}
		if w == nil && p != nil && s.app.PaneStatusKey(pid) == "blocked" {
			w = &screenPromptWatch{signature: p.Signature(), prompt: p, lastMatched: true}
			s.screenPrompts.watches[pid] = w
			if p.HasPreview && !s.reuseScreenPrompt(pid, w) {
				s.startPreviewScan(pid, w)
			} else {
				s.createScreenPrompt(pid, w)
			}
		}
	}
}

func (s *Server) reuseScreenPrompt(pid int, w *screenPromptWatch) bool {
	store, err := s.db()
	if err != nil {
		return false
	}
	pending, err := store.Decisions("pending")
	if err != nil {
		return false
	}
	for _, d := range pending {
		if d.Origin != nil && d.Origin.Reason == "screen_prompt" && d.Origin.PaneID == fmtPaneID(pid) && screenprompt.FromDecision(d) == w.signature {
			w.decisionID = d.DecisionID
			return true
		}
	}
	return false
}

func (s *Server) createScreenPrompt(pid int, w *screenPromptWatch) {
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		s.app.SetConfigWarning(err.Error())
		return
	}
	// Read persisted pending records only on first discovery, including restart/handoff.
	pending, err := store.Decisions("pending")
	if err != nil {
		s.app.SetConfigWarning(err.Error())
		return
	}
	for _, d := range pending {
		if d.Origin != nil && d.Origin.Reason == "screen_prompt" && d.Origin.PaneID == fmtPaneID(pid) && screenprompt.FromDecision(d) == w.signature {
			w.decisionID = d.DecisionID
			return
		}
	}
	p := w.prompt.Params
	pane := s.sup.Get(pid)
	if pane == nil {
		return
	}
	sn := pane.Snapshot()
	agent := sn.AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	p.Origin = &api.DecisionOrigin{PaneID: fmtPaneID(pid), Agent: agent, Cwd: sn.Cwd, Machine: "local", Reason: "screen_prompt"}
	d, err := store.CreateDecision(p, time.Now())
	if err != nil {
		s.app.SetConfigWarning("screen prompt creation: " + err.Error())
		return
	}
	w.decisionID = d.DecisionID
	s.decisionChanged("decision_created", d)
}
