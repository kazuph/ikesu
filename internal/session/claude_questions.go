package session

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ClaudeQuestion struct {
	SessionID string
	ToolID    string
	Index     int
	Header    string
	Question  string
	Multi     bool
	Answered  bool
	Options   []ClaudeQuestionOption
}

type ClaudeQuestionOption struct{ Label, Description string }

// PendingClaudeQuestions refuses ambiguous files and incomplete records.
func PendingClaudeQuestions(root, sessionID string) ([]ClaudeQuestion, bool) {
	return new(ClaudeQuestionCache).Read(root, sessionID)
}

// ClaudeQuestionCache is owned by one scanner; callers must not share it across goroutines.
type ClaudeQuestionCache struct {
	path      string
	sessionID string
	size      int64
	modified  time.Time
	questions []ClaudeQuestion
	valid     bool
}

func (cache *ClaudeQuestionCache) Read(root, sessionID string) ([]ClaudeQuestion, bool) {
	all, ok := cache.ReadAll(root, sessionID)
	if !ok {
		return nil, false
	}
	var pending []ClaudeQuestion
	for _, q := range all {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	return pending, true
}

func (cache *ClaudeQuestionCache) ReadAll(root, sessionID string) ([]ClaudeQuestion, bool) {
	if !SafeSessionID(sessionID) {
		return nil, false
	}
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, "projects"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == sessionID+".jsonl" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil || len(paths) != 1 {
		return nil, false
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		return nil, false
	}
	if cache.valid && cache.path == paths[0] && cache.sessionID == sessionID && cache.size == info.Size() && cache.modified.Equal(info.ModTime()) {
		return cache.questions, true
	}
	f, err := os.Open(paths[0])
	if err != nil {
		return nil, false
	}
	defer f.Close()
	type option struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	type question struct {
		Header   string   `json:"header"`
		Question string   `json:"question"`
		Multi    bool     `json:"multiSelect"`
		Options  []option `json:"options"`
	}
	type block struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		ToolUseID string          `json:"tool_use_id"`
		Input     json.RawMessage `json:"input"`
	}
	type row struct {
		SessionID string `json:"sessionId"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	pending := map[string][]ClaudeQuestion{}
	answered := map[string]bool{}
	matched := false
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		} // A partial final line is never authoritative.
		if err != nil {
			return nil, false
		}
		var record row
		if json.Unmarshal(line, &record) != nil {
			return nil, false
		}
		if record.SessionID != "" && record.SessionID != sessionID {
			return nil, false
		}
		if record.SessionID == sessionID {
			matched = true
		}
		if len(record.Message.Content) == 0 || !strings.HasPrefix(strings.TrimSpace(string(record.Message.Content)), "[") {
			continue
		}
		var blocks []block
		if json.Unmarshal(record.Message.Content, &blocks) != nil {
			return nil, false
		}
		for _, b := range blocks {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				answered[b.ToolUseID] = true
			}
			if b.Type != "tool_use" || b.Name != "AskUserQuestion" || b.ID == "" {
				continue
			}
			var input struct {
				Questions []question `json:"questions"`
			}
			if json.Unmarshal(b.Input, &input) != nil {
				return nil, false
			}
			qs := make([]ClaudeQuestion, 0, len(input.Questions))
			for index, q := range input.Questions {
				item := ClaudeQuestion{SessionID: sessionID, ToolID: b.ID, Index: index, Header: q.Header, Question: q.Question, Multi: q.Multi}
				for _, o := range q.Options {
					item.Options = append(item.Options, ClaudeQuestionOption{Label: o.Label, Description: o.Description})
				}
				qs = append(qs, item)
			}
			pending[b.ID] = qs
		}
	}
	var out []ClaudeQuestion
	for id, questions := range pending {
		for _, q := range questions {
			q.Answered = answered[id]
			out = append(out, q)
		}
	}
	if matched {
		cache.path, cache.sessionID, cache.size, cache.modified, cache.questions, cache.valid = paths[0], sessionID, info.Size(), info.ModTime(), out, true
	}
	return out, matched
}
