package server

import (
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

type screenPromptHub struct {
	seen        map[int]map[string]bool
	rebuilds    map[int][]time.Time
	flows       map[int]*screenAnswerFlow
	watches     map[int]*screenPromptWatch
	transcripts map[int]*claudeTranscriptScan
	late        map[string]*lateCodexQuestion
	verifying   map[string]*lateCodexQuestion
	timer       *time.Timer
}

// Waiting screens use the review's one-second bound; preview navigation keeps
// the established short poll while arrow keys are advancing the screen.
const screenPromptWaitPoll = time.Second
const claudeTranscriptWait = 60 * time.Second

type screenPromptWatch struct {
	codex               *codexBinding
	codexResult         <-chan codexBindingResult
	codexAnchorPath     string
	codexAnchorSession  string
	codexAnchorStart    time.Time
	codexAnchorGroup    int
	codexChecked        bool
	codexStateResult    <-chan codexStateResult
	transcriptDeadline  time.Time
	permissionDeadline  time.Time
	signature           string
	transcriptID        string
	displayParams       *api.DecisionCreateParams
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
	if current != nil && current.MCPServer != "" {
		return current
	}
	if expanded != nil && expanded.MCPServer != "" {
		return nil
	}
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
	if len(s.screenPrompts.verifying) > 0 {
		return true
	}
	for _, late := range s.screenPrompts.late {
		if late.delivery != nil && late.delivery.Status == "pending" {
			return true
		}
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
	s.screenPrompts.verifying = nil
	for id, late := range s.screenPrompts.late {
		if late.delivery != nil && late.delivery.Status == "pending" {
			s.setLateDelivery(id, "failed", "画面の質問の検出が無効になりました")
		}
	}
	for _, w := range s.screenPrompts.watches {
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
	s.scanLateCodexDeliveries()
	s.scanVerifyingCodexQuestions()
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
		if w != nil && w.editingText && w.prompt != nil && w.prompt.Params.Origin != nil && w.prompt.Params.Origin.Tool != "" {
			continue
		}
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
		transcriptScreen := p
		if transcriptScreen == nil && w != nil && w.transcriptID != "" {
			transcriptScreen = w.prompt
		}
		transcriptID, display, transcriptOK, transcriptPending, transcriptUnverified, transcriptResolved := s.pollClaudeTranscript(pid, transcriptScreen)
		if transcriptResolved {
			if w != nil {
				s.cancelScreenPrompt(w)
				delete(s.screenPrompts.watches, pid)
			}
			continue
		}
		if w != nil && w.codex != nil && w.decisionID != "" && !w.resolved && p != nil && p.Signature() != w.signature {
			if s.screenPrompts.verifying == nil {
				s.screenPrompts.verifying = map[string]*lateCodexQuestion{}
			}
			s.screenPrompts.verifying[w.decisionID] = &lateCodexQuestion{pid: pid, w: w}
			delete(s.screenPrompts.watches, pid)
			w = nil
		}
		if w != nil && w.codex != nil && w.decisionID != "" && !w.resolved {
			state, ready, nextCall, questions := pollBoundCodexState(w)
			if !ready && p == nil {
				continue
			}
			if ready && (p == nil || nextCall) {
				if state == session.CodexEmpty || state == session.CodexAnswered {
					s.retainOtherEmptyCodexQuestions(pid, w, questions)
				}
				switch state {
				case session.CodexEmpty:
					if s.retainLateCodexQuestion(pid, w) {
						delete(s.screenPrompts.watches, pid)
						w = nil
					}
				case session.CodexAnswered:
					s.closeCodexAnsweredElsewhere(w)
					delete(s.screenPrompts.watches, pid)
					w = nil
				}
			}
		}
		if w != nil {
			if p == nil && !w.resolved && w.transcriptID != "" && (transcriptPending || transcriptOK && transcriptID == w.transcriptID) {
				w.lastMatched = false
				w.mismatches = 0
				continue
			}
			if !w.resolved && w.transcriptID != "" && transcriptUnverified {
				w.lastMatched = false
				w.mismatches = 0
				continue
			}
			if p == nil && w.prompt != nil && w.prompt.CodexQuestion && (!w.resolved || w.editingText) {
				if w.decisionID != "" && !w.codexChecked {
					s.waitCodexBinding(pid, w)
				}
				if pane := s.sup.Get(pid); pane != nil && !pane.Snapshot().Dead {
					w.lastMatched = false
					w.mismatches = 0
					continue
				}
			}
			if p != nil && !w.resolved && transcriptOK {
				same := w.transcriptID == transcriptID
				if w.transcriptID == "" {
					same = matchesClaudeDisplay(w.prompt, p, display)
				}
				if same && w.transcriptID == "" && w.decisionID != "" {
					store, err := s.db()
					if err == nil {
						err = store.BindScreenQuestion(w.decisionID, fmtPaneID(pid), transcriptID)
					}
					if err != nil {
						s.app.SetConfigWarning("screen question identity: " + err.Error())
						same = false
					}
				}
				if same {
					w.signature = p.Signature()
					w.transcriptID = transcriptID
					w.displayParams = &display
				}
			}
			changedIdentity := transcriptOK && w.transcriptID != "" && transcriptID != w.transcriptID
			if transcriptPending && w.transcriptID != "" && p != nil && p.Signature() != w.signature {
				w.lastMatched = false
				continue
			}
			if p != nil && !w.resolved && w.decisionID != "" && w.transcriptID == "" && !transcriptOK && p.MCPServer == w.prompt.MCPServer && p.Signature() != w.signature && s.screenRebuildFlood(pid) && p.ChoiceSignature() == w.prompt.ChoiceSignature() {
				w.signature = p.Signature()
				w.prompt = p
				s.updateScreenPrompt(w)
			}

			if p == nil || changedIdentity || p.Signature() != w.signature {
				w.lastMatched = false
				w.mismatches++
				if w.preview != nil {
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
					if s.waitCodexBinding(pid, w) {
						continue
					}
					if s.waitClaudeQuestion(pid, w, transcriptOK, transcriptPending, transcriptUnverified) {
						continue
					}
					s.beginScreenPrompt(pid, w)
					continue
				}
				if !w.resolved {
					if w.prompt.CodexQuestion && !w.codexChecked {
						s.waitCodexBinding(pid, w)
					}
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
			if p.Params.Origin != nil && p.Params.Origin.Tool != "" && p.Params.TimeoutMS != nil {
				w.permissionDeadline = time.Now().Add(time.Duration(*p.Params.TimeoutMS) * time.Millisecond)
			}
			if s.isClaudeQuestion(pid, p) {
				w.transcriptDeadline = time.Now().Add(claudeTranscriptWait)
			}
			if transcriptOK {
				w.transcriptID, w.displayParams = transcriptID, &display
			}
			s.screenPrompts.watches[pid] = w
			if s.waitCodexBinding(pid, w) {
				continue
			}
			if s.waitClaudeQuestion(pid, w, transcriptOK, transcriptPending, transcriptUnverified) {
				continue
			}
			s.beginScreenPrompt(pid, w)
		}
	}
}

func (s *Server) beginScreenPrompt(pid int, w *screenPromptWatch) {
	if s.reuseScreenPrompt(pid, w) {
		return
	}
	if w.prompt.HasPreview && !s.previewSeen(pid, w.prompt) {
		s.markPreviewSeen(pid, w.prompt)
		s.startPreviewScan(pid, w)
		return
	}
	s.createScreenPrompt(pid, w)
}

func (s *Server) reuseScreenPrompt(pid int, w *screenPromptWatch) bool {
	store, err := s.db()
	if err != nil {
		return false
	}
	if w.codex != nil {
		d, found, err := store.ScreenQuestionDecision(fmtPaneID(pid), w.codex.identity)
		if err != nil {
			s.app.SetConfigWarning("screen question lookup: " + err.Error())
			return false
		}
		if found {
			w.decisionID = d.DecisionID
		}
		return found
	}
	if w.transcriptID != "" {
		d, found, err := store.ScreenQuestionDecision(fmtPaneID(pid), w.transcriptID)
		if err != nil {
			s.app.SetConfigWarning("screen question lookup: " + err.Error())
			return false
		}
		if found {
			w.decisionID = d.DecisionID
			s.updateScreenPrompt(w)
			return true
		}
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
	if s.reuseScreenPrompt(pid, w) {
		return
	}
	p := screenDisplayParams(w)
	if !w.permissionDeadline.IsZero() {
		remaining := time.Until(w.permissionDeadline)
		ms := uint64(max(0, remaining.Milliseconds()))
		p.TimeoutMS = &ms
	} else if p.Origin != nil && p.Origin.Tool != "" {
		p.TimeoutMS = nil
	}
	pane := s.sup.Get(pid)
	if pane == nil {
		return
	}
	sn := pane.Snapshot()
	agent := sn.AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	if p.Origin == nil {
		p.Origin = &api.DecisionOrigin{}
	}
	p.Origin.PaneID, p.Origin.Agent, p.Origin.Cwd, p.Origin.Machine, p.Origin.Reason = fmtPaneID(pid), w.prompt.OriginAgent(agent), sn.Cwd, "local", "screen_prompt"
	s.completeDecisionOrigin(p.Origin)
	var d api.Decision
	if w.codex != nil {
		d, err = store.CreateScreenDecision(p, time.Now(), fmtPaneID(pid), w.codex.identity)
	} else if w.transcriptID != "" {
		d, err = store.CreateScreenDecision(p, time.Now(), fmtPaneID(pid), w.transcriptID)
	} else {
		d, err = store.CreateDecision(p, time.Now())
	}
	if err != nil {
		s.app.SetConfigWarning("screen prompt creation: " + err.Error())
		return
	}
	w.decisionID = d.DecisionID
	s.decisionChanged("decision_created", d)
}
