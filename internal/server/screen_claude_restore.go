package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

func (s *Server) captureClaudeQuestionContext(pid int, decisionID string) {
	pane := s.sup.Get(pid)
	if pane == nil || s.screenPrompts.claudeCapturing[decisionID] {
		return
	}
	if s.screenPrompts.claudeRestore == nil {
		s.screenPrompts.claudeRestore = map[string]int{}
	}
	if s.screenPrompts.claudeCapturing == nil {
		s.screenPrompts.claudeCapturing = map[string]bool{}
	}
	s.screenPrompts.claudeRestore[decisionID] = pid
	s.screenPrompts.claudeCapturing[decisionID] = true
	s.scheduleScreenPrompts()
	go func() {
		group := pane.ForegroundJob().ProcessGroupID
		id, start, ok := observedClaudeSession(pane)
		s.app.Do(func() {
			delete(s.screenPrompts.claudeCapturing, decisionID)
			if !s.cfg.ScreenPrompts() || s.sup.Get(pid) != pane || pane.Snapshot().Dead {
				return
			}
			if !ok {
				return
			}
			current := pane.ForegroundJob()
			currentStart, live := session.ProcessStart(current)
			if !live || group <= 0 || current.ProcessGroupID != group || !currentStart.Equal(start) || foregroundAgent(pane) != "claude" {
				return
			}
			d, err := s.store.Decision(decisionID)
			if err != nil || d.Status != "pending" || start.UnixMilli() > d.CreatedUnixMS {
				delete(s.screenPrompts.claudeRestore, decisionID)
				return
			}
			if err := s.store.BindClaudeScreenContext(decisionID, fmtPaneID(pid), id, start); err != nil {
				return
			}
			s.screenPrompts.claudeRestore[decisionID] = pid
			s.scheduleScreenPrompts()
		})
	}()
}

func observedClaudeSession(pane *runtime.Pane) (string, time.Time, bool) {
	if pane.Snapshot().Dead || foregroundAgent(pane) != "claude" {
		return "", time.Time{}, false
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		return "", time.Time{}, false
	}
	snapshot := pane.Snapshot()
	id, ok := session.ObserveProcessSession("claude", snapshot.Cwd, start, job, pane.SessionEnvironment())
	if !ok {
		return "", time.Time{}, false
	}
	current := pane.ForegroundJob()
	currentStart, ok := session.ProcessStart(current)
	if !ok || current.ProcessGroupID != job.ProcessGroupID || !currentStart.Equal(start) || pane.Snapshot().Cwd != snapshot.Cwd || foregroundAgent(pane) != "claude" {
		return "", time.Time{}, false
	}
	return id, start, true
}

func restoredUnidentifiedClaudeQuestion(pane *runtime.Pane, expected string, processStart time.Time, d api.Decision) (string, bool) {
	id, start, ok := observedClaudeSession(pane)
	if !ok || id != expected || start.UnixMilli() != processStart.UnixMilli() || start.UnixMilli() > d.CreatedUnixMS {
		return "", false
	}
	home, root := "", ""
	for _, item := range pane.SessionEnvironment() {
		key, value, found := strings.Cut(item, "=")
		if found && key == "HOME" {
			home = value
		}
		if found && key == "CLAUDE_CONFIG_DIR" {
			root = value
		}
	}
	if root == "" {
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		root = filepath.Join(home, ".claude")
	}
	questions, ok := new(session.ClaudeQuestionCache).ReadAll(root, id)
	if !ok {
		return "", false
	}
	currentID, currentStart, ok := observedClaudeSession(pane)
	if !ok || currentID != id || currentStart.UnixMilli() != start.UnixMilli() {
		return "", false
	}
	p := &screenprompt.Prompt{Params: api.DecisionCreateParams{Kind: d.Kind, Title: d.Title, Body: d.Body, Options: d.Options, MultiSelect: d.MultiSelect, QuestionLabel: d.QuestionLabel}}
	var found *session.ClaudeQuestion
	for i := range questions {
		q := &questions[i]
		if q.StartedAt.IsZero() || q.StartedAt.Before(start) || q.StartedAt.UnixMilli() >= d.CreatedUnixMS || (q.Answered && (q.AnsweredAt.IsZero() || q.AnsweredAt.UnixMilli() <= d.CreatedUnixMS)) || !matchClaudeQuestion(p, *q) {
			continue
		}
		if found != nil {
			return "", false
		}
		found = q
	}
	if found == nil {
		return "", false
	}
	identity, _ := json.Marshal([]any{id, found.ToolID, found.Index})
	if found.Answered {
		return string(identity), true
	}
	visible := screenprompt.Parse("claude", pane.DetectionScreenLines())
	for _, next := range questions {
		if next.ToolID != found.ToolID && !next.StartedAt.IsZero() && next.StartedAt.UnixMilli() > d.CreatedUnixMS && matchClaudeQuestion(visible, next) {
			return string(identity), true
		}
	}
	return string(identity), false
}

// A visible match alone cannot identify a decision created before a later call.
func (s *Server) claudeDecisionMatchesIdentity(pid int, decisionID, identity string) bool {
	pane := s.sup.Get(pid)
	if pane == nil || s.store == nil {
		return false
	}
	paneID, sessionID, start, found, err := s.store.ClaudeScreenContext(decisionID)
	if err != nil || !found || paneID != fmtPaneID(pid) {
		return false
	}
	d, err := s.store.Decision(decisionID)
	if err != nil || d.Status != "pending" {
		return false
	}
	identified, done := restoredUnidentifiedClaudeQuestion(pane, sessionID, start, d)
	return !done && identified == identity
}

// Only identified records can be associated safely with an answered call.
func (s *Server) prepareClaudeQuestionRestore() {
	if s.store == nil {
		return
	}
	pending, err := s.store.Decisions("pending")
	if err != nil {
		s.app.SetConfigWarning("Claude question restore: " + err.Error())
		return
	}
	if s.screenPrompts.claudeRestore == nil {
		s.screenPrompts.claudeRestore = map[string]int{}
	}
	for _, d := range pending {
		if d.Kind != "agent_prompt" || d.Origin == nil || d.Origin.Agent != "claude" || d.Origin.Reason != "screen_prompt" || d.Origin.Tool != "" || (d.Origin.Machine != "" && d.Origin.Machine != "local") {
			continue
		}
		pid, ok := parsePaneID(d.Origin.PaneID)
		if !ok || s.sup.Get(pid) == nil {
			continue
		}
		identity, err := s.store.ScreenQuestionIdentity(d.DecisionID)
		if err != nil {
			s.app.SetConfigWarning("Claude question restore: " + err.Error())
			continue
		}
		if identity == "" {
			contextPane, _, _, found, err := s.store.ClaudeScreenContext(d.DecisionID)
			if err == nil && (!found || contextPane == d.Origin.PaneID) {
				s.screenPrompts.claudeRestore[d.DecisionID] = pid
			}
			continue
		}
		var parts []json.RawMessage
		if json.Unmarshal([]byte(identity), &parts) != nil || len(parts) != 3 {
			continue
		}
		var sessionID, toolID string
		var index int
		if json.Unmarshal(parts[0], &sessionID) != nil || json.Unmarshal(parts[1], &toolID) != nil || json.Unmarshal(parts[2], &index) != nil || sessionID == "" || toolID == "" || index < 0 {
			continue
		}
		s.screenPrompts.claudeRestore[d.DecisionID] = pid
	}
}

func (s *Server) scanRestoredClaudeQuestions() {
	if s.screenPrompts.claudeRestoring || len(s.screenPrompts.claudeRestore) == 0 {
		return
	}
	type record struct {
		id, identity string
		pid          int
		pane         *runtime.Pane
		decision     api.Decision
		sessionID    string
		processStart time.Time
	}
	var records []record
	for id, pid := range s.screenPrompts.claudeRestore {
		d, err := s.store.Decision(id)
		if err != nil || d.Status != "pending" {
			delete(s.screenPrompts.claudeRestore, id)
			continue
		}
		pane := s.sup.Get(pid)
		if pane == nil || pane.Snapshot().Dead {
			delete(s.screenPrompts.claudeRestore, id)
			continue
		}
		identity, err := s.store.ScreenQuestionIdentity(id)
		if err == nil {
			r := record{id: id, identity: identity, pid: pid, pane: pane, decision: d}
			if identity == "" {
				contextPane, sessionID, start, found, e := s.store.ClaudeScreenContext(id)
				if e != nil {
					continue
				}
				if !found {
					s.captureClaudeQuestionContext(pid, id)
					continue
				}
				if contextPane != fmtPaneID(pid) {
					continue
				}
				r.sessionID, r.processStart = sessionID, start
			}
			records = append(records, r)
		}
	}
	if len(records) == 0 {
		return
	}
	s.screenPrompts.claudeRestoring = true
	go func() {
		answered := map[string]bool{}
		identified := map[string]string{}
		for _, r := range records {
			if r.identity == "" {
				identity, done := restoredUnidentifiedClaudeQuestion(r.pane, r.sessionID, r.processStart, r.decision)
				if identity != "" {
					identified[r.id], answered[r.id] = identity, done
				}
				continue
			}
			var identity struct {
				session, tool string
				index         int
			}
			var parts []json.RawMessage
			if json.Unmarshal([]byte(r.identity), &parts) != nil || len(parts) != 3 || json.Unmarshal(parts[0], &identity.session) != nil || json.Unmarshal(parts[1], &identity.tool) != nil || json.Unmarshal(parts[2], &identity.index) != nil {
				continue
			}
			if restoredClaudeAnswered(r.pane, identity.session, identity.tool, identity.index) {
				answered[r.id] = true
			}
		}
		s.app.Do(func() {
			s.screenPrompts.claudeRestoring = false
			for _, r := range records {
				if s.screenPrompts.claudeRestore[r.id] != r.pid || s.sup.Get(r.pid) != r.pane {
					continue
				}
				identity, err := s.store.ScreenQuestionIdentity(r.id)
				if err != nil || identity != r.identity {
					continue
				}
				if identity == "" {
					if identified[r.id] == "" {
						continue
					}
					if err := s.store.BindScreenQuestion(r.id, fmtPaneID(r.pid), identified[r.id]); err != nil {
						owner, found, lookupErr := s.store.ScreenQuestionDecisionAny(fmtPaneID(r.pid), identified[r.id])
						if lookupErr != nil || !found || owner.DecisionID == r.id || !answered[r.id] {
							continue
						}
					}
				}
				if !answered[r.id] {
					continue
				}
				w := &screenPromptWatch{decisionID: r.id}
				s.cancelScreenPrompt(w)
				if w.resolved {
					delete(s.screenPrompts.claudeRestore, r.id)
				}
			}
		})
	}()
}

func restoredClaudeAnswered(pane *runtime.Pane, expected, tool string, index int) bool {
	if pane.Snapshot().Dead || foregroundAgent(pane) != "claude" {
		return false
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		return false
	}
	snapshot := pane.Snapshot()
	env := pane.SessionEnvironment()
	id, ok := session.ObserveProcessSession("claude", snapshot.Cwd, start, job, env)
	if !ok || id != expected {
		return false
	}
	home, root := "", ""
	for _, item := range env {
		key, value, found := strings.Cut(item, "=")
		if found && key == "HOME" {
			home = value
		}
		if found && key == "CLAUDE_CONFIG_DIR" {
			root = value
		}
	}
	if root == "" {
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		root = filepath.Join(home, ".claude")
	}
	questions, ok := new(session.ClaudeQuestionCache).ReadAll(root, id)
	if !ok {
		return false
	}
	current := pane.ForegroundJob()
	currentStart, ok := session.ProcessStart(current)
	if !ok || current.ProcessGroupID != job.ProcessGroupID || !currentStart.Equal(start) || pane.Snapshot().Cwd != snapshot.Cwd || foregroundAgent(pane) != "claude" {
		return false
	}
	currentID, ok := session.ObserveProcessSession("claude", snapshot.Cwd, currentStart, current, pane.SessionEnvironment())
	if !ok || currentID != id {
		return false
	}
	var previous *session.ClaudeQuestion
	for i := range questions {
		q := &questions[i]
		if q.ToolID == tool && q.Index == index {
			if q.Answered {
				return true
			}
			previous = q
			break
		}
	}
	if previous == nil || previous.StartedAt.IsZero() {
		return false
	}
	visible := screenprompt.Parse("claude", pane.DetectionScreenLines())
	for _, next := range questions {
		if next.ToolID != tool && next.StartedAt.After(previous.StartedAt) && matchClaudeQuestion(visible, next) {
			return true
		}
	}
	return false
}
