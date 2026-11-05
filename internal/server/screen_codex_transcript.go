package server

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

var codexQuestionPosition = regexp.MustCompile(`Question\s+(\d+)/(\d+)`)

type codexBinding struct {
	path     string
	question session.CodexQuestion
	identity string
}

type lateCodexQuestion struct {
	pid      int
	w        *screenPromptWatch
	delivery *api.DecisionDelivery
	checking bool
	sent     bool
	offset   int64
	fileInfo os.FileInfo
}

type codexStateResult struct {
	state    session.CodexAnswerState
	nextCall bool
}

func codexQuestionScan(binding codexBinding, prompt *screenprompt.Prompt, questions []session.CodexQuestion) codexStateResult {
	next, ok := chooseCodexQuestion(prompt, questions)
	return codexStateResult{state: boundCodexState(binding.question, questions), nextCall: ok && next.CallID != binding.question.CallID}
}

func (s *Server) withLateDelivery(d api.Decision) api.Decision {
	if late := s.screenPrompts.late[d.DecisionID]; late != nil && late.delivery != nil {
		copy := *late.delivery
		d.Delivery = &copy
	}
	return d
}

func lateCodexAnswerText(d api.Decision, answer api.DecisionAnswer) (string, error) {
	ids := answer.OptionIDs
	if len(ids) == 0 && answer.OptionID != nil {
		ids = []string{*answer.OptionID}
	}
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		found := false
		for _, option := range d.Options {
			if option.ID == id {
				labels = append(labels, option.Label)
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("unknown answer option %q", id)
		}
	}
	value := strings.Join(labels, "、")
	if answer.Text != nil && *answer.Text != "" {
		if value == "" {
			value = *answer.Text
		} else {
			value += "（メモ：" + *answer.Text + "）"
		}
	}
	if value == "" {
		return "", fmt.Errorf("empty answer")
	}
	return "さっきの質問『" + d.Title + "』への回答：" + value, nil
}

func (s *Server) retainLateCodexQuestion(pid int, w *screenPromptWatch) bool {
	store, err := s.db()
	if err != nil {
		s.app.SetConfigWarning("late question: " + err.Error())
		return false
	}
	p := screenDisplayParams(w)
	body := "Codex は空の答えで先に進みました。答えると会話で届けます"
	if p.Body != nil && *p.Body != "" {
		body = *p.Body + "\n\n" + body
	}
	p.Body = &body
	d, changed, err := store.UpdatePendingScreenDecision(w.decisionID, p)
	if err != nil {
		s.app.SetConfigWarning("late question: " + err.Error())
		return false
	}
	if d.Status != "pending" { w.resolved = true; return true }
	if s.screenPrompts.late == nil {
		s.screenPrompts.late = map[string]*lateCodexQuestion{}
	}
	s.screenPrompts.late[w.decisionID] = &lateCodexQuestion{pid: pid, w: w}
	if changed {
		s.pushDecisionProjection()
	}
	return true
}

func boundCodexState(bound session.CodexQuestion, questions []session.CodexQuestion) session.CodexAnswerState {
	var state session.CodexAnswerState
	for _, q := range questions {
		if q.SessionID != bound.SessionID || q.CallID != bound.CallID || q.ID != bound.ID {
			continue
		}
		if state != "" {
			return ""
		}
		state = q.State
	}
	return state
}

func pollBoundCodexState(w *screenPromptWatch) (session.CodexAnswerState, bool, bool) {
	if w.codex == nil {
		return "", true, false
	}
	if w.codexStateResult == nil {
		result := make(chan codexStateResult, 1)
		w.codexStateResult = result
		binding := *w.codex
		prompt := *w.prompt
		prompt.Params.Options = append(prompt.Params.Options[:0:0], w.prompt.Params.Options...)
		go func() {
			questions, err := session.ReadCodexQuestions(binding.path, binding.question.SessionID)
			if err != nil {
				result <- codexStateResult{}
				return
			}
			result <- codexQuestionScan(binding, &prompt, questions)
		}()
		return "", false, false
	}
	select {
	case result := <-w.codexStateResult:
		w.codexStateResult = nil
		return result.state, true, result.nextCall
	default:
		return "", false, false
	}
}

func (s *Server) scanVerifyingCodexQuestions() {
	for id, old := range s.screenPrompts.verifying {
		pane := s.sup.Get(old.pid)
		if pane == nil || pane.Snapshot().Dead {
			s.cancelScreenPrompt(old.w)
			delete(s.screenPrompts.verifying, id)
			continue
		}
		state, ready, _ := pollBoundCodexState(old.w)
		if !ready {
			continue
		}
		switch state {
		case session.CodexEmpty:
			if s.retainLateCodexQuestion(old.pid, old.w) {
				delete(s.screenPrompts.verifying, id)
			}
		case session.CodexAnswered:
			s.cancelScreenPrompt(old.w)
			delete(s.screenPrompts.verifying, id)
		}
	}
}

func readCodexBinding(pane *runtime.Pane, prompt *screenprompt.Prompt) *codexBinding {
	if pane == nil || pane.Snapshot().Dead || foregroundAgent(pane) != "codex" {
		return nil
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		return nil
	}
	path, id, err := session.OpenCodexRollout(job)
	if err != nil {
		return nil
	}
	questions, err := session.ReadCodexQuestions(path, id)
	if err != nil {
		return nil
	}
	q, ok := chooseCodexQuestion(prompt, questions)
	if !ok {
		return nil
	}
	current := pane.ForegroundJob()
	currentStart, ok := session.ProcessStart(current)
	if !ok || current.ProcessGroupID != job.ProcessGroupID || !currentStart.Equal(start) || foregroundAgent(pane) != "codex" {
		return nil
	}
	currentPath, currentID, err := session.OpenCodexRollout(current)
	if err != nil || currentPath != path || currentID != id {
		return nil
	}
	identity, _ := json.Marshal([]string{id, q.CallID, q.ID})
	return &codexBinding{path: path, question: q, identity: string(identity)}
}

func (s *Server) waitCodexBinding(pid int, w *screenPromptWatch) bool {
	if w.prompt == nil || !w.prompt.CodexQuestion || w.codexChecked {
		return false
	}
	if w.codexResult == nil {
		pane := s.sup.Get(pid)
		if pane == nil || foregroundAgent(pane) != "codex" {
			w.codexChecked = true
			return false
		}
		prompt := *w.prompt
		prompt.Params.Options = append(prompt.Params.Options[:0:0], w.prompt.Params.Options...)
		result := make(chan *codexBinding, 1)
		w.codexResult = result
		go func() { result <- readCodexBinding(pane, &prompt) }()
		return true
	}
	select {
	case w.codex = <-w.codexResult:
		w.codexChecked = true
		return false
	default:
		return true
	}
}

// Match only the unique unanswered call whose visible position and content agree.
func chooseCodexQuestion(p *screenprompt.Prompt, questions []session.CodexQuestion) (session.CodexQuestion, bool) {
	var found session.CodexQuestion
	if p == nil || !p.CodexQuestion || p.Params.QuestionLabel == nil {
		return found, false
	}
	position := codexQuestionPosition.FindStringSubmatch(*p.Params.QuestionLabel)
	if len(position) != 3 {
		return found, false
	}
	index, err1 := strconv.Atoi(position[1])
	total, err2 := strconv.Atoi(position[2])
	if err1 != nil || err2 != nil {
		return found, false
	}
	matched := false
	for _, q := range questions {
		if q.State != session.CodexPending || q.Index+1 != index || q.Total != total || compactQuestion(p.Params.Title) != compactQuestion(q.Question) || len(p.Params.Options) != len(q.Options) {
			continue
		}
		same := true
		for i, option := range p.Params.Options {
			if compactQuestion(option.Label) != compactQuestion(q.Options[i].Label) || option.Description != nil && compactQuestion(*option.Description) != compactQuestion(q.Options[i].Description) {
				same = false
				break
			}
		}
		if !same {
			continue
		}
		if matched {
			return session.CodexQuestion{}, false
		}
		found, matched = q, true
	}
	return found, matched
}
