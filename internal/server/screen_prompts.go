package server

import (
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/screenprompt"
)

type screenPromptHub struct {
	seen     map[int]map[string]bool
	rebuilds map[int][]time.Time
	flows    map[int]*screenAnswerFlow
	watches  map[int]*screenPromptWatch
	timer    *time.Timer
}

// Waiting screens use the review's one-second bound; preview navigation keeps
// the established short poll while arrow keys are advancing the screen.
const screenPromptWaitPoll = time.Second

type screenPromptWatch struct {
	signature           string
	decisionID          string
	prompt              *screenprompt.Prompt
	mismatches          int
	lastMatched         bool
	resolved            bool
	preview             *screenPreviewScan
	previews            map[string]*string
	delivery            <-chan error
	answerInputSequence uint64
	editingText         bool
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
	textID := ""
	if w := s.screenPrompts.watches[pid]; w != nil && w.editingText {
		textID = w.prompt.TextID
	}
	visible, _ := p.ReadLines("visible", true)
	current := screenprompt.ParseWithTextID(agent, p.DetectionScreenLines(), textID)
	if w := s.screenPrompts.watches[pid]; w != nil && w.prompt != nil && w.prompt.CodexQuestion && current == nil {
		return nil
	}
	if !screenprompt.HasOptionRows(visible) {
		return nil
	}
	expanded := screenprompt.ParseWithTextID(agent, p.DetectionLines(), textID)
	footer := strings.ToLower(strings.Join(visible, " "))
	if expanded != nil && expanded.Params.Kind == "tool_permission" && (current != nil && current.Params.Kind == "tool_permission" || current == nil && strings.Contains(footer, "esc") && strings.Contains(footer, "cancel")) {
		return expanded
	}
	return current
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
		active = active || s.screenPromptPaneActive(pid)
	})
	return active
}

func (s *Server) screenPromptPaneActive(pid int) bool {
	if s.screenPrompts.watches[pid] != nil {
		return true
	}
	p := s.sup.Get(pid)
	if p == nil || p.Snapshot().Dead {
		return false
	}
	agent := p.Snapshot().AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	return (agent == "claude" || agent == "codex" || agent == "devin") && s.app.PaneStatusKey(pid) == "blocked"
}

// Output and status hooks inspect only their pane. Reservations return before
// reading even that pane's state.
func (s *Server) scheduleScreenPromptPane(pid int) {
	if s.screenPrompts.timer != nil || !s.cfg.ScreenPrompts() || !s.screenPromptPaneActive(pid) {
		return
	}
	s.armScreenPromptTimer()
}

func (s *Server) screenPromptPollInterval() time.Duration {
	for _, w := range s.screenPrompts.watches {
		if w.preview != nil {
			return eventWaitPoll
		}
	}
	return screenPromptWaitPoll
}

func (s *Server) scheduleScreenPrompts() {
	if s.screenPrompts.timer != nil || !s.screenPromptsActive() {
		return
	}
	s.armScreenPromptTimer()
}

func (s *Server) armScreenPromptTimer() {
	s.initDecisionHub()
	s.screenPrompts.timer = time.AfterFunc(s.screenPromptPollInterval(), func() {
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
		if s.screenUserActive(pid) {
			if p != nil {
				s.markPreviewSeen(pid, p)
			}
			if w != nil && w.preview != nil {
				s.cachePreviewScan(w)
			}
			continue
		}
		if p != nil && p.Final {
			s.submitScreenFlow(pid, p)
			p = nil
		}
		if w != nil {
			if p == nil && w.prompt != nil && w.prompt.CodexQuestion && (!w.resolved || w.editingText) {
				if pane := s.sup.Get(pid); pane != nil && !pane.Snapshot().Dead {
					w.lastMatched = false
					w.mismatches = 0
					continue
				}
			}
			if p != nil && !w.resolved && p.Signature() != w.signature && s.screenRebuildFlood(pid) && p.ChoiceSignature() == w.prompt.ChoiceSignature() {
				w.signature = p.Signature()
				w.prompt = p
				s.updateScreenPrompt(w)
			}

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
				if p != nil && !w.resolved {
					s.noteScreenRebuild(pid)
				}
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
					s.updateScreenPrompt(w)
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
			if p.HasPreview && !s.reuseScreenPrompt(pid, w) && !s.previewSeen(pid, p) {
				s.markPreviewSeen(pid, p)
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
	s.completeDecisionOrigin(p.Origin)
	d, err := store.CreateDecision(p, time.Now())
	if err != nil {
		s.app.SetConfigWarning("screen prompt creation: " + err.Error())
		return
	}
	w.decisionID = d.DecisionID
	s.decisionChanged("decision_created", d)
}
