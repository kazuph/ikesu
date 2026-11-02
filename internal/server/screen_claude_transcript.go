package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

func compactQuestion(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func claudeQuestionPresentation(p api.DecisionCreateParams, q session.ClaudeQuestion) api.DecisionCreateParams {
	p.Title = q.Question
	return p
}

func matchClaudeQuestion(p *screenprompt.Prompt, q session.ClaudeQuestion) bool {
	if p == nil || p.CodexQuestion || p.MCPServer != "" || p.Params.Kind != "agent_prompt" || p.Params.MultiSelect != q.Multi || len(p.Params.Options) != len(q.Options) || q.Question == "" {
		return false
	}
	if p.Total > 0 && (p.Question != q.Index || p.Total <= q.Index) {
		return false
	}
	screenTitle := compactQuestion(p.Params.Title)
	if screenTitle == "" || !strings.Contains(compactQuestion(q.Question), screenTitle) {
		return false
	}
	for i, option := range p.Params.Options {
		label := compactQuestion(option.Label)
		if label == "" || compactQuestion(q.Options[i].Label) != label {
			return false
		}
	}
	return true
}

func matchesClaudeDisplay(previous, current *screenprompt.Prompt, display api.DecisionCreateParams) bool {
	q := session.ClaudeQuestion{Question: display.Title, Multi: display.MultiSelect, Index: current.Question}
	for _, option := range display.Options {
		q.Options = append(q.Options, session.ClaudeQuestionOption{Label: option.Label})
	}
	return matchClaudeQuestion(previous, q)
}

func chooseClaudeQuestion(p *screenprompt.Prompt, questions []session.ClaudeQuestion) (*session.ClaudeQuestion, bool, bool) {
	var match *session.ClaudeQuestion
	pendingCount := 0
	resolvedMatch := false
	for i := range questions {
		if !questions[i].Answered {
			pendingCount++
		}
		if !matchClaudeQuestion(p, questions[i]) {
			continue
		}
		if questions[i].Answered {
			resolvedMatch = true
			continue
		}
		if match != nil {
			return nil, true, false
		}
		match = &questions[i]
	}
	if match == nil {
		return nil, pendingCount > 0, resolvedMatch
	}
	return match, false, false
}

func (s *Server) claudeTranscriptPrompt(pid int, p *screenprompt.Prompt) (string, api.DecisionCreateParams, bool, bool, bool) {
	pane := s.sup.Get(pid)
	if pane == nil || p == nil || p.Params.Kind != "agent_prompt" || p.MCPServer != "" || p.CodexQuestion || pane.Snapshot().Dead {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	snapshot := pane.Snapshot()
	agent := snapshot.AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	return readClaudeTranscript(pane, p, agent, nil)
}

func readClaudeTranscript(pane *runtime.Pane, p *screenprompt.Prompt, agent string, cache *session.ClaudeQuestionCache) (string, api.DecisionCreateParams, bool, bool, bool) {
	snapshot := pane.Snapshot()
	if agent != "claude" || foregroundAgent(pane) != "claude" {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	job := pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	env := pane.SessionEnvironment()
	id, ok := session.ObserveProcessSession("claude", snapshot.Cwd, start, job, env)
	if !ok {
		return "", api.DecisionCreateParams{}, false, false, false
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
	if cache == nil {
		cache = new(session.ClaudeQuestionCache)
	}
	questions, ok := cache.ReadAll(root, id)
	if !ok {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	currentJob := pane.ForegroundJob()
	currentStart, live := session.ProcessStart(currentJob)
	if !live || !currentStart.Equal(start) || currentJob.ProcessGroupID != job.ProcessGroupID || pane.Snapshot().Cwd != snapshot.Cwd {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	currentID, live := session.ObserveProcessSession("claude", snapshot.Cwd, currentStart, currentJob, pane.SessionEnvironment())
	if !live || currentID != id {
		return "", api.DecisionCreateParams{}, false, false, false
	}
	match, unverified, resolvedMatch := chooseClaudeQuestion(p, questions)
	if match == nil {
		return "", api.DecisionCreateParams{}, false, unverified, resolvedMatch
	}
	params := p.Params
	params.Title = match.Question
	params.Options = append([]api.DecisionOption(nil), p.Params.Options...)
	for i := range params.Options {
		params.Options[i].Label = match.Options[i].Label
		description := match.Options[i].Description
		params.Options[i].Description = &description
	}
	identity, _ := json.Marshal([]any{id, match.ToolID, match.Index})
	return string(identity), params, true, false, false
}

type claudeTranscriptResult struct {
	signature  string
	id         string
	params     api.DecisionCreateParams
	ok         bool
	unverified bool
	resolved   bool
}
type claudeTranscriptScan struct {
	result chan claudeTranscriptResult
	latest claudeTranscriptResult
	cache  session.ClaudeQuestionCache
}

// Transcript discovery and reading can be slow on large histories, so the app
// loop only consumes completed scans and never waits for filesystem I/O.
func (s *Server) pollClaudeTranscript(pid int, p *screenprompt.Prompt) (string, api.DecisionCreateParams, bool, bool, bool, bool) {
	if p == nil || p.Params.Kind != "agent_prompt" || p.MCPServer != "" || p.CodexQuestion {
		return "", api.DecisionCreateParams{}, false, false, false, false
	}
	pane := s.sup.Get(pid)
	if pane == nil || pane.Snapshot().Dead {
		return "", api.DecisionCreateParams{}, false, false, false, false
	}
	agent := pane.Snapshot().AgentID
	if reported, _, ok := s.app.Reported(pid); ok && reported != "" {
		agent = reported
	}
	if agent != "claude" || foregroundAgent(pane) != "claude" {
		return "", api.DecisionCreateParams{}, false, false, false, false
	}
	if s.screenPrompts.transcripts == nil {
		s.screenPrompts.transcripts = map[int]*claudeTranscriptScan{}
	}
	scan := s.screenPrompts.transcripts[pid]
	if scan == nil {
		scan = &claudeTranscriptScan{}
		s.screenPrompts.transcripts[pid] = scan
	}
	if scan.result != nil {
		select {
		case scan.latest = <-scan.result:
			scan.result = nil
		default:
		}
	}
	signature := p.Signature()
	if scan.result == nil {
		scan.result = make(chan claudeTranscriptResult, 1)
		result := scan.result
		copy := *p
		copy.Params.Options = append([]api.DecisionOption(nil), p.Params.Options...)
		go func() {
			id, params, ok, unverified, resolved := readClaudeTranscript(pane, &copy, agent, &scan.cache)
			result <- claudeTranscriptResult{signature: signature, id: id, params: params, ok: ok, unverified: unverified, resolved: resolved}
		}()
	}
	if scan.latest.signature == signature {
		return scan.latest.id, scan.latest.params, scan.latest.ok, false, scan.latest.unverified, scan.latest.resolved
	}
	return "", api.DecisionCreateParams{}, false, true, false, false
}
