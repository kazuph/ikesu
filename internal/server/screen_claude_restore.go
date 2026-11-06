package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
)

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
			records = append(records, record{id, identity, pid, pane})
		}
	}
	if len(records) == 0 {
		return
	}
	s.screenPrompts.claudeRestoring = true
	go func() {
		answered := map[string]bool{}
		for _, r := range records {
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
				if !answered[r.id] || s.screenPrompts.claudeRestore[r.id] != r.pid || s.sup.Get(r.pid) != r.pane {
					continue
				}
				identity, err := s.store.ScreenQuestionIdentity(r.id)
				if err != nil || identity != r.identity {
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
	for _, q := range questions {
		if q.ToolID == tool && q.Index == index && q.Answered {
			return true
		}
	}
	return false
}
