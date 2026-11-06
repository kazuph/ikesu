package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kazuph/ikesu/internal/platform"
)

type CodexAnswerState string

const (
	CodexPending  CodexAnswerState = "pending"
	CodexEmpty    CodexAnswerState = "empty"
	CodexAnswered CodexAnswerState = "answered"
)

type CodexQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type CodexQuestion struct {
	Timestamp time.Time
	SessionID string
	CallID    string
	ID        string
	Index     int
	Total     int
	Header    string
	Question  string
	Options   []CodexQuestionOption
	State     CodexAnswerState
}

// OpenCodexRollout requires one JSONL file held open by the foreground job.
// It never searches by working directory or session-file timestamps.
func OpenCodexRollout(job platform.ForegroundJob) (string, string, error) {
	paths := map[string]bool{}
	for _, process := range job.Processes {
		for _, path := range platform.OpenJSONLFiles(process.PID) {
			paths[path] = true
		}
	}
	if len(paths) != 1 {
		return "", "", errors.New("foreground rollout is missing or ambiguous")
	}
	for path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return "", "", err
		}
		line, err := bufio.NewReader(f).ReadBytes('\n')
		f.Close()
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				ID        string `json:"id"`
				SessionID string `json:"session_id"`
			} `json:"payload"`
		}
		if err != nil || json.Unmarshal(line, &record) != nil || record.Type != "session_meta" {
			return "", "", errors.New("unreadable rollout identity")
		}
		id := record.Payload.ID
		if id == "" {
			id = record.Payload.SessionID
		}
		if id == "" {
			return "", "", errors.New("rollout identity missing")
		}
		return path, id, nil
	}
	return "", "", errors.New("foreground rollout missing")
}

// HasCodexUserInput only accepts complete user-message records appended after offset.
func HasCodexUserInput(path string, offset int64, text string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if offset < 0 || info.Size() < offset {
		return false, errors.New("rollout was truncated")
	}
	if offset > 0 {
		if _, err = f.Seek(offset-1, io.SeekStart); err != nil {
			return false, err
		}
		var last [1]byte
		if _, err = io.ReadFull(f, last[:]); err != nil || last[0] != '\n' {
			return false, errors.New("rollout checkpoint is incomplete")
		}
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return false, err
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var r struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		// Ignore unrelated record payload shapes, but never accept malformed JSON.
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &envelope) != nil {
			return false, errors.New("invalid rollout record")
		}
		if envelope.Type != "response_item" {
			continue
		}
		var kind struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if json.Unmarshal(envelope.Payload, &kind) != nil {
			return false, errors.New("invalid response item")
		}
		if kind.Type != "message" || kind.Role != "user" {
			continue
		}
		if json.Unmarshal(line, &r) != nil {
			return false, errors.New("invalid user message")
		}
		for _, content := range r.Payload.Content {
			if content.Type == "input_text" && content.Text == text {
				return true, nil
			}
		}
	}
}

// ReadCodexQuestions only accepts complete records from the specified session.
func ReadCodexQuestions(path, sessionID string) ([]CodexQuestion, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type question struct {
		ID       string                `json:"id"`
		Header   string                `json:"header"`
		Question string                `json:"question"`
		Options  []CodexQuestionOption `json:"options"`
	}
	type row struct {
		Timestamp time.Time `json:"timestamp"`
		Type      string    `json:"type"`
		Payload   struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			SessionID string          `json:"session_id"`
			Name      string          `json:"name"`
			CallID    string          `json:"call_id"`
			Arguments json.RawMessage `json:"arguments"`
			Output    json.RawMessage `json:"output"`
		} `json:"payload"`
	}
	var questions []CodexQuestion
	calls := map[string][]int{}
	outputs := map[string]bool{}
	matched := false
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil {
			return nil, errors.New("incomplete or unreadable rollout record")
		}
		var record row
		if json.Unmarshal(line, &record) != nil {
			return nil, errors.New("invalid rollout record")
		}
		if record.Type == "session_meta" {
			id := record.Payload.ID
			if id == "" {
				id = record.Payload.SessionID
			}
			if id != sessionID || matched {
				return nil, errors.New("rollout session mismatch")
			}
			matched = true
		}
		if record.Type != "response_item" {
			continue
		}
		p := record.Payload
		if p.Type == "function_call" && p.Name == "request_user_input" {
			var args struct {
				Questions []question `json:"questions"`
			}
			var arguments string
			if p.CallID == "" || calls[p.CallID] != nil || json.Unmarshal(p.Arguments, &arguments) != nil || json.Unmarshal([]byte(arguments), &args) != nil || len(args.Questions) == 0 {
				return nil, errors.New("invalid question call")
			}
			ids := map[string]bool{}
			for i, q := range args.Questions {
				if q.ID == "" || q.Question == "" || ids[q.ID] {
					return nil, errors.New("ambiguous question identity")
				}
				ids[q.ID] = true
				calls[p.CallID] = append(calls[p.CallID], len(questions))
				questions = append(questions, CodexQuestion{Timestamp: record.Timestamp, SessionID: sessionID, CallID: p.CallID, ID: q.ID, Index: i, Total: len(args.Questions), Header: q.Header, Question: q.Question, Options: q.Options, State: CodexPending})
			}
		} else if p.Type == "function_call_output" && calls[p.CallID] != nil {
			if outputs[p.CallID] {
				return nil, errors.New("duplicate question output")
			}
			outputs[p.CallID] = true
			var result struct {
				Answers map[string]json.RawMessage `json:"answers"`
			}
			var output string
			if json.Unmarshal(p.Output, &output) != nil || json.Unmarshal([]byte(output), &result) != nil || result.Answers == nil {
				return nil, fmt.Errorf("invalid output for %s", p.CallID)
			}
			known := map[string]bool{}
			for _, i := range calls[p.CallID] {
				known[questions[i].ID] = true
			}
			for id := range result.Answers {
				if !known[id] {
					return nil, fmt.Errorf("unknown answer id %s", id)
				}
			}
			for _, i := range calls[p.CallID] {
				questions[i].State = CodexEmpty
				raw, exists := result.Answers[questions[i].ID]
				if !exists {
					continue
				}
				var entry struct {
					Answers *[]string `json:"answers"`
				}
				if json.Unmarshal(raw, &entry) != nil || entry.Answers == nil {
					return nil, fmt.Errorf("invalid answer for %s", questions[i].ID)
				}
				for _, answer := range *entry.Answers {
					if answer != "" {
						questions[i].State = CodexAnswered
						break
					}
				}
			}
		}
	}
	if !matched {
		return nil, errors.New("rollout session missing")
	}
	return questions, nil
}
