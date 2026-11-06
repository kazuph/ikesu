package server

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	state     session.CodexAnswerState
	nextCall  bool
	questions []session.CodexQuestion
}

func codexQuestionScan(binding codexBinding, prompt *screenprompt.Prompt, questions []session.CodexQuestion) codexStateResult {
	next, ok := chooseCodexQuestion(prompt, questions)
	return codexStateResult{state: boundCodexState(binding.question, questions), nextCall: ok && next.CallID != binding.question.CallID, questions: questions}
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
	if p.Body != nil && *p.Body != "" && *p.Body != body && !strings.HasPrefix(*p.Body, "auto-resolves in ") {
		original := strings.TrimSuffix(*p.Body, "\n\n"+body)
		body = original + "\n\n" + body
	}
	p.Body = &body
	d, _, err := store.UpdatePendingScreenDecision(w.decisionID, p)
	if err != nil {
		s.app.SetConfigWarning("late question: " + err.Error())
		return false
	}
	if d.Status != "pending" {
		w.resolved = true
		return true
	}
	if s.screenPrompts.late == nil {
		s.screenPrompts.late = map[string]*lateCodexQuestion{}
	}
	s.screenPrompts.late[w.decisionID] = &lateCodexQuestion{pid: pid, w: w}
	s.syncDecisionAttention()
	s.pushDecisionProjection()
	return true
}

func (s *Server) closeCodexAnsweredElsewhere(w *screenPromptWatch) {
	store, err := s.db()
	if err == nil {
		params := screenDisplayParams(w)
		body := "ほかで回答済み"
		params.Body = &body
		_, _, err = store.UpdatePendingScreenDecision(w.decisionID, params)
	}
	if err != nil {
		s.app.SetConfigWarning("screen question: " + err.Error())
		return
	}
	s.cancelScreenPrompt(w)
}

// A single call can contain several questions, including ones the terminal
// never displayed before the empty result. Retain each independently.
func (s *Server) retainOtherEmptyCodexQuestions(pid int, w *screenPromptWatch, questions []session.CodexQuestion) {
	store, err := s.db()
	if err != nil {
		s.app.SetConfigWarning("late question: " + err.Error())
		return
	}
	original, err := store.Decision(w.decisionID)
	if err != nil || original.Origin == nil {
		return
	}
	for _, q := range questions {
		if q.SessionID != w.codex.question.SessionID || q.CallID != w.codex.question.CallID || q.ID == w.codex.question.ID || q.State != session.CodexEmpty {
			continue
		}
		raw, err := json.Marshal([]string{q.SessionID, q.CallID, q.ID})
		if err != nil {
			continue
		}
		identity := string(raw)
		_, found, err := store.ScreenQuestionDecisionAny(fmtPaneID(pid), identity)
		if err != nil {
			s.app.SetConfigWarning("late question: " + err.Error())
			continue
		}
		if found {
			continue
		}
		body := "Codex は空の答えで先に進みました。答えると会話で届けます"
		label := fmt.Sprintf("Question %d/%d", q.Index+1, q.Total)
		params := api.DecisionCreateParams{Kind: "agent_prompt", Title: q.Question, Body: &body, QuestionLabel: &label, AllowText: true, Origin: original.Origin}
		for i, option := range q.Options {
			description := option.Description
			params.Options = append(params.Options, api.DecisionOption{ID: strconv.Itoa(i + 1), Label: option.Label, Role: "other", Description: &description})
		}
		d, err := store.CreateScreenDecision(params, time.Now(), fmtPaneID(pid), identity)
		if err != nil {
			s.app.SetConfigWarning("late question: " + err.Error())
			continue
		}
		binding := *w.codex
		binding.question, binding.identity = q, identity
		if s.screenPrompts.late == nil {
			s.screenPrompts.late = map[string]*lateCodexQuestion{}
		}
		s.screenPrompts.late[d.DecisionID] = &lateCodexQuestion{pid: pid, w: &screenPromptWatch{decisionID: d.DecisionID, codex: &binding}}
		s.decisionChanged("decision_created", d)
	}
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

func pollBoundCodexState(w *screenPromptWatch) (session.CodexAnswerState, bool, bool, []session.CodexQuestion) {
	if w.codex == nil {
		return "", true, false, nil
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
		return "", false, false, nil
	}
	select {
	case result := <-w.codexStateResult:
		w.codexStateResult = nil
		return result.state, true, result.nextCall, result.questions
	default:
		return "", false, false, nil
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
		state, ready, _, questions := pollBoundCodexState(old.w)
		if !ready {
			continue
		}
		if state == session.CodexEmpty || state == session.CodexAnswered {
			s.retainOtherEmptyCodexQuestions(old.pid, old.w, questions)
		}
		switch state {
		case session.CodexEmpty:
			if s.retainLateCodexQuestion(old.pid, old.w) {
				delete(s.screenPrompts.verifying, id)
			}
		case session.CodexAnswered:
			s.closeCodexAnsweredElsewhere(old.w)
			delete(s.screenPrompts.verifying, id)
		}
	}
}

type codexBindingResult struct {
	binding         *codexBinding
	path, sessionID string
	start           time.Time
	group           int
}

func readCodexBinding(pane *runtime.Pane, prompt *screenprompt.Prompt, cutoff int64) codexBindingResult {
	if pane == nil || pane.Snapshot().Dead || foregroundAgent(pane) != "codex" {
		return codexBindingResult{}
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		return codexBindingResult{}
	}
	path, id, err := session.OpenCodexRollout(job)
	if err != nil {
		return codexBindingResult{}
	}
	questions, readErr := session.ReadCodexQuestions(path, id)
	current := pane.ForegroundJob()
	currentStart, ok := session.ProcessStart(current)
	if !ok || current.ProcessGroupID != job.ProcessGroupID || !currentStart.Equal(start) || foregroundAgent(pane) != "codex" {
		return codexBindingResult{}
	}
	currentPath, currentID, err := session.OpenCodexRollout(current)
	if err != nil || currentPath != path || currentID != id {
		return codexBindingResult{}
	}
	result := codexBindingResult{path: path, sessionID: id, start: start, group: job.ProcessGroupID}
	if readErr != nil {
		return result
	}
	var q session.CodexQuestion
	if cutoff == 0 {
		var ok bool
		q, ok = chooseCodexQuestion(prompt, questions)
		if !ok {
			return result
		}
	} else {
		count := 0
		for _, candidate := range questions {
			if candidate.Timestamp.IsZero() || candidate.Timestamp.UnixMilli() > cutoff {
				continue
			}
			match := candidate
			match.State = session.CodexPending
			if _, ok := chooseCodexQuestion(prompt, []session.CodexQuestion{match}); ok {
				q = candidate
				count++
			}
		}
		if count != 1 {
			return result
		}
	}
	identity, _ := json.Marshal([]string{id, q.CallID, q.ID})
	result.binding = &codexBinding{path: path, question: q, identity: string(identity)}
	return result
}

func (s *Server) waitCodexBinding(pid int, w *screenPromptWatch) bool {
	if w.prompt == nil || !w.prompt.CodexQuestion || w.codexChecked {
		return false
	}
	if w.codexResult == nil {
		pane := s.sup.Get(pid)
		if pane == nil || foregroundAgent(pane) != "codex" {
			return false
		}
		prompt := *w.prompt
		prompt.Params.Options = append(prompt.Params.Options[:0:0], w.prompt.Params.Options...)
		var cutoff int64
		if w.decisionID != "" {
			store, err := s.db()
			if err != nil {
				return false
			}
			d, err := store.Decision(w.decisionID)
			if err != nil || d.Status != "pending" {
				return false
			}
			cutoff = d.CreatedUnixMS
		}
		result := make(chan codexBindingResult, 1)
		w.codexResult = result
		go func() { result <- readCodexBinding(pane, &prompt, cutoff) }()
		return w.decisionID == ""
	}
	select {
	case result := <-w.codexResult:
		w.codexResult = nil
		if w.codexAnchorPath == "" && result.path != "" && w.decisionID == "" {
			w.codexAnchorPath, w.codexAnchorSession, w.codexAnchorStart, w.codexAnchorGroup = result.path, result.sessionID, result.start, result.group
		}
		if result.binding == nil {
			return false
		}
		if w.decisionID != "" {
			if w.codexAnchorPath == "" || result.path != w.codexAnchorPath || result.sessionID != w.codexAnchorSession || result.group != w.codexAnchorGroup || !result.start.Equal(w.codexAnchorStart) {
				return false
			}
			store, err := s.db()
			if err != nil {
				return false
			}
			d, err := store.Decision(w.decisionID)
			if err != nil || d.Status != "pending" || result.binding.question.Timestamp.IsZero() || result.binding.question.Timestamp.UnixMilli() > d.CreatedUnixMS {
				return false
			}
			if err := store.BindScreenQuestion(d.DecisionID, fmtPaneID(pid), result.binding.identity); err != nil {
				return false
			}
		}
		w.codex, w.codexChecked = result.binding, true
		return false
	default:
		return w.decisionID == ""
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
