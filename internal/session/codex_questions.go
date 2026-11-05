package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

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
	SessionID string
	CallID    string
	ID        string
	Index     int
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
		Type    string `json:"type"`
		Payload struct {
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
				questions = append(questions, CodexQuestion{SessionID: sessionID, CallID: p.CallID, ID: q.ID, Index: i, Header: q.Header, Question: q.Question, Options: q.Options, State: CodexPending})
			}
		} else if p.Type == "function_call_output" && calls[p.CallID] != nil {
			if outputs[p.CallID] {
				return nil, errors.New("duplicate question output")
			}
			outputs[p.CallID] = true
			var result struct {
				Answers map[string]struct {
					Answers []string `json:"answers"`
				} `json:"answers"`
			}
			var output string
			if json.Unmarshal(p.Output, &output) != nil || json.Unmarshal([]byte(output), &result) != nil || result.Answers == nil {
				return nil, fmt.Errorf("invalid output for %s", p.CallID)
			}
			for _, i := range calls[p.CallID] {
				answers := result.Answers[questions[i].ID].Answers
				questions[i].State = CodexEmpty
				for _, answer := range answers {
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
