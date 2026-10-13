package server

import (
	"fmt"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/screenprompt"
)

func screenOrigin(d api.Decision) bool { return d.Origin != nil && d.Origin.Reason == "screen_prompt" }

func (s *Server) validateScreenAnswer(d api.Decision, p api.DecisionAnswerParams) (*screenPromptWatch, int, error) {
	if !screenOrigin(d) || d.Status != "pending" {
		return nil, 0, nil
	}
	pid, _, _, ok := s.findPane(d.Origin.PaneID)
	w := s.screenPrompts.watches[pid]
	current := s.readScreenPrompt(pid)
	damaged := false
	if pane := s.sup.Get(pid); pane != nil {
		damaged = screenprompt.HasOptionRows(pane.DetectionLines())
	}
	if !ok || w == nil || w.decisionID != d.DecisionID || !s.cfg.ScreenPrompts() || current != nil && current.Signature() != w.signature || current == nil && (!w.lastMatched || !damaged) || s.sup.Get(pid) == nil || s.sup.Get(pid).Snapshot().Dead {
		if w == nil {
			w = &screenPromptWatch{decisionID: d.DecisionID}
		}
		s.cancelScreenPrompt(w)
		return nil, 0, &api.Error{Code: api.ErrDecisionStale, Message: "screen prompt no longer matches this decision"}
	}
	if current != nil {
		w.prompt = current
	}
	answer, err := dispatch.ValidateDecisionAnswer(d, p)
	if err != nil {
		return nil, 0, err
	}
	if d.MultiSelect && (w.prompt.Progress < 0 || w.prompt.Cursor < 0) {
		return nil, 0, &api.Error{Code: "screen_prompt_unsupported", Message: "multi-select prompt has no supported cursor or Next/Submit row"}
	}
	if !d.MultiSelect && len(answer.OptionIDs) > 1 {
		return nil, 0, &api.Error{Code: "screen_prompt_unsupported", Message: "single-choice screen cannot submit multiple options"}
	}
	if p.Text != nil && w.prompt.TextID == "" {
		return nil, 0, &api.Error{Code: "screen_prompt_unsupported", Message: "free input row is unavailable"}
	}
	return w, pid, nil
}

func (s *Server) startScreenAnswer(pid int, w *screenPromptWatch, answer api.DecisionAnswer) {
	result := make(chan error, 1)
	w.delivery = result
	w.resolved = true
	prompt := w.prompt
	go func() {
		err := s.deliverScreenAnswer(pid, w.signature, prompt, answer)
		if err != nil {
			s.app.Do(func() { s.app.SetConfigWarning("screen prompt input failed: " + err.Error()) })
		}
		result <- err
	}()
}

// Each write is a separate PTY event; delay reuses the established agent prompt submit interval.
func (s *Server) screenInput(pid int, signature, data string, verify bool) error {
	err := fmt.Errorf("server stopped before input delivery")
	s.app.Do(func() {
		pane := s.sup.Get(pid)
		if pane == nil || pane.Snapshot().Dead || !s.cfg.ScreenPrompts() {
			err = fmt.Errorf("pane closed or screen prompts disabled")
			return
		}
		if verify {
			p := s.readScreenPrompt(pid)
			if p == nil || p.Signature() != signature {
				err = fmt.Errorf("question changed during input delivery")
				return
			}
		}
		_, err = pane.WriteToPTY([]byte(data))
	})
	return err
}

func (s *Server) screenInputDelay() error {
	timer := time.NewTimer(agentPromptSubmitDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-s.decisions.stopped:
		return fmt.Errorf("server stopped during input delivery")
	}
}

func (s *Server) deliverScreenAnswer(pid int, signature string, prompt *screenprompt.Prompt, answer api.DecisionAnswer) error {
	if !prompt.Params.MultiSelect {
		if answer.Text == nil {
			if answer.OptionID == nil {
				return fmt.Errorf("missing option")
			}
			return s.screenInput(pid, signature, prompt.Keys[*answer.OptionID], false)
		}
		for _, data := range []string{prompt.TextID, *answer.Text, "\r"} {
			if err := s.screenInput(pid, signature, data, false); err != nil {
				return err
			}
			if err := s.screenInputDelay(); err != nil {
				return err
			}
		}
		return nil
	}
	ids := answer.OptionIDs
	if len(ids) == 0 && answer.OptionID != nil {
		ids = []string{*answer.OptionID}
	}
	desired := map[string]bool{}
	for _, id := range ids {
		desired[id] = true
	}
	for _, o := range prompt.Params.Options {
		if prompt.Checked[o.ID] == desired[o.ID] {
			continue
		}
		if err := s.screenInput(pid, signature, o.ID, true); err != nil {
			return err
		}
		if err := s.screenInputDelay(); err != nil {
			return err
		}
	}
	if answer.Text != nil {
		for _, data := range []string{prompt.TextID, *answer.Text, "\r"} {
			if err := s.screenInput(pid, signature, data, false); err != nil {
				return err
			}
			if err := s.screenInputDelay(); err != nil {
				return err
			}
		}
	}
	// Re-read after toggles: number keys may have moved the cursor.
	for {
		var current *screenprompt.Prompt
		s.app.Do(func() { current = s.readScreenPrompt(pid) })
		if current == nil || current.Signature() != signature || current.Progress < 0 || current.Cursor < 0 {
			return fmt.Errorf("cannot locate multi-select progress row")
		}
		for _, o := range current.Params.Options {
			if current.Checked[o.ID] != desired[o.ID] {
				return fmt.Errorf("multi-select check state did not match the answer")
			}
		}
		if current.Cursor == current.Progress {
			return s.screenInput(pid, signature, "\r", true)
		}
		arrow := "\x1b[B"
		next := current.Cursor + 1
		if current.Progress < current.Cursor {
			arrow = "\x1b[A"
			next = current.Cursor - 1
		}
		if err := s.screenInput(pid, signature, arrow, true); err != nil {
			return err
		}
		if err := s.screenInputDelay(); err != nil {
			return err
		}
		var after *screenprompt.Prompt
		s.app.Do(func() { after = s.readScreenPrompt(pid) })
		if after == nil || after.Signature() != signature || after.Cursor != next {
			return fmt.Errorf("progress navigation did not advance")
		}
	}
}
