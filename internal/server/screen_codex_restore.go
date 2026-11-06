package server

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

// The observed screen-to-decision delay was 7.731s and the rollout-call-to-decision
// delay was 11.029s; 15s leaves 3.971s beyond the latter measurement.
const legacyCodexQuestionWindow = 15 * time.Second

// restoreLateCodexQuestions only examines decisions that existed at startup.
// An uncertain match remains pending and keeps its attention indicator.
func (s *Server) restoreLateCodexQuestions() {
	if !s.cfg.ScreenPrompts() || s.store == nil {
		return
	}
	pending, err := s.store.Decisions("pending")
	if err != nil {
		s.app.SetConfigWarning("late question restore: " + err.Error())
		return
	}
	for _, d := range pending {
		if d.Kind != "agent_prompt" || d.Origin == nil || d.Origin.Agent != "codex" || d.Origin.Reason != "screen_prompt" || (d.Origin.Machine != "" && d.Origin.Machine != "local") {
			continue
		}
		pid, _, _, ok := s.findPane(d.Origin.PaneID)
		if !ok {
			fmt.Fprintf(os.Stderr, "ikesu: late question restore %s: pane unavailable\n", d.DecisionID)
			continue
		}
		pane := s.sup.Get(pid)
		if pane == nil {
			continue
		}
		identity, err := s.store.ScreenQuestionIdentity(d.DecisionID)
		if err != nil {
			s.app.SetConfigWarning("late question restore: " + err.Error())
			continue
		}
		go s.readRestoredCodexQuestion(pid, pane, d, identity, pending)
	}
}

func restoredCodexMatch(d api.Decision, questions []session.CodexQuestion, identity string) (session.CodexQuestion, bool) {
	prompt := &screenprompt.Prompt{CodexQuestion: true, Params: api.DecisionCreateParams{Title: d.Title, Options: d.Options, QuestionLabel: d.QuestionLabel}}
	matches := []session.CodexQuestion{}
	for _, q := range questions {
		if identity == "" {
			if q.Timestamp.IsZero() || d.CreatedUnixMS <= 0 {
				continue
			}
			elapsed := time.UnixMilli(d.CreatedUnixMS).Sub(q.Timestamp)
			if elapsed < 0 || elapsed > legacyCodexQuestionWindow {
				continue
			}
		}
		raw, _ := json.Marshal([]string{q.SessionID, q.CallID, q.ID})
		if identity != "" && identity != string(raw) {
			continue
		}
		candidate := q
		candidate.State = session.CodexPending
		if _, ok := chooseCodexQuestion(prompt, []session.CodexQuestion{candidate}); ok {
			matches = append(matches, q)
		}
	}
	if len(matches) != 1 || matches[0].State != session.CodexEmpty {
		return session.CodexQuestion{}, false
	}
	return matches[0], true
}

func (s *Server) readRestoredCodexQuestion(pid int, pane *runtime.Pane, d api.Decision, identity string, pending []api.Decision) {
	fail := func(reason string) {
		fmt.Fprintf(os.Stderr, "ikesu: late question restore %s: %s\n", d.DecisionID, reason)
	}
	if pane.Snapshot().Dead || foregroundAgent(pane) != "codex" {
		fail("foreground Codex unavailable")
		return
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		fail("foreground process identity unavailable")
		return
	}
	path, id, err := session.OpenCodexRollout(job)
	if err != nil {
		fail("foreground rollout unavailable or ambiguous")
		return
	}
	questions, err := session.ReadCodexQuestions(path, id)
	if err != nil {
		fail("rollout unreadable")
		return
	}
	q, ok := restoredCodexMatch(d, questions, identity)
	if !ok {
		fail("question or empty result not uniquely verified")
		return
	}

	if identity == "" {
		for _, other := range pending {
			if other.DecisionID == d.DecisionID || other.Origin == nil || other.Origin.PaneID != d.Origin.PaneID || other.Origin.Reason != "screen_prompt" {
				continue
			}
			if _, matched := restoredCodexMatch(other, []session.CodexQuestion{q}, ""); matched {
				fail("multiple pending decisions match question")
				return
			}
		}
	}
	current := pane.ForegroundJob()
	currentStart, ok := session.ProcessStart(current)
	if !ok || current.ProcessGroupID != job.ProcessGroupID || !currentStart.Equal(start) || foregroundAgent(pane) != "codex" {
		fail("foreground process changed")
		return
	}
	path2, id2, err := session.OpenCodexRollout(current)
	if err != nil || path2 != path || id2 != id {
		fail("foreground session changed")
		return
	}
	raw, _ := json.Marshal([]string{id, q.CallID, q.ID})
	s.app.Do(func() {
		if !s.cfg.ScreenPrompts() || s.sup.Get(pid) != pane || pane.Snapshot().Dead || s.screenPrompts.late[d.DecisionID] != nil {
			return
		}
		saved, err := s.store.Decision(d.DecisionID)
		if err != nil || saved.Status != "pending" {
			return
		}
		if visible := s.readScreenPrompt(pid); visible != nil && visible.CodexQuestion {
			candidate := q
			candidate.State = session.CodexPending
			if _, same := chooseCodexQuestion(visible, []session.CodexQuestion{candidate}); same {
				fail("question remains visible")
				return
			}
		}
		if err := s.store.BindScreenQuestion(d.DecisionID, d.Origin.PaneID, string(raw)); err != nil {
			fail("question identity conflicts")
			return
		}
		prompt := &screenprompt.Prompt{CodexQuestion: true, Params: api.DecisionCreateParams{Kind: d.Kind, Title: d.Title, Body: d.Body, Options: d.Options, AllowText: d.AllowText, MultiSelect: d.MultiSelect, QuestionLabel: d.QuestionLabel}}
		w := &screenPromptWatch{decisionID: d.DecisionID, prompt: prompt, codex: &codexBinding{path: path, question: q, identity: string(raw)}}
		s.retainLateCodexQuestion(pid, w)
	})
}
