package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cellwidth"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

type claudePermissionRecord struct {
	Type               string `json:"type"`
	ToolDenialKind     string `json:"toolDenialKind"`
	UserFeedback       string `json:"userFeedback"`
	PermissionDecision *struct {
		Decision string `json:"decision"`
	} `json:"permissionDecision"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func claudePermissionPath(pane *runtime.Pane) (string, os.FileInfo, error) {
	snapshot, job := pane.Snapshot(), pane.ForegroundJob()
	start, ok := session.ProcessStart(job)
	if !ok || foregroundAgent(pane) != "claude" {
		return "", nil, fmt.Errorf("Claude process unavailable")
	}
	id, ok := session.ObserveProcessSession("claude", snapshot.Cwd, start, job, pane.SessionEnvironment())
	if !ok || !session.SafeSessionID(id) {
		return "", nil, fmt.Errorf("Claude session unavailable")
	}
	root, home := "", ""
	for _, item := range pane.SessionEnvironment() {
		key, value, _ := strings.Cut(item, "=")
		if key == "CLAUDE_CONFIG_DIR" {
			root = value
		}
		if key == "HOME" {
			home = value
		}
	}
	if root == "" {
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		root = filepath.Join(home, ".claude")
	}
	paths, err := filepath.Glob(filepath.Join(root, "projects", "*", id+".jsonl"))
	if err != nil || len(paths) != 1 {
		return "", nil, fmt.Errorf("Claude transcript is not unique")
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		return "", nil, err
	}
	return paths[0], info, nil
}

func claudePermissionReceipt(path, toolID, text string, previous os.FileInfo) (bool, error) {
	info, err := os.Stat(path)
	if err != nil || previous == nil || !os.SameFile(info, previous) || info.Size() < previous.Size() {
		return false, fmt.Errorf("Claude transcript changed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data[previous.Size():]), "\n") {
		var row claudePermissionRecord
		if json.Unmarshal([]byte(line), &row) != nil || row.Type != "user" || row.ToolDenialKind != "user-rejected" || row.UserFeedback != text || row.PermissionDecision != nil && row.PermissionDecision.Decision != "reject" {
			continue
		}
		var blocks []struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if json.Unmarshal(row.Message.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type == "tool_result" && block.ToolUseID == toolID && block.IsError {
				return true, nil
			}
		}
	}
	return false, nil
}

func claudePermissionReceiptAny(path, text string, previous os.FileInfo) (bool, error) {
	info, err := os.Stat(path)
	if err != nil || previous == nil || !os.SameFile(info, previous) || info.Size() < previous.Size() {
		return false, fmt.Errorf("Claude transcript changed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data[previous.Size():]), "\n") {
		var row claudePermissionRecord
		if json.Unmarshal([]byte(line), &row) != nil || row.Type != "user" || row.ToolDenialKind != "user-rejected" || row.UserFeedback != text || row.PermissionDecision != nil && row.PermissionDecision.Decision != "reject" {
			continue
		}
		var results []struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if json.Unmarshal(row.Message.Content, &results) != nil {
			continue
		}
		for _, result := range results {
			if result.Type == "tool_result" && result.ToolUseID != "" && result.IsError {
				return true, nil
			}
		}
	}
	return false, nil
}

func claudePermissionEditMatches(original, current *screenprompt.Prompt, selected, text string, screen []string) bool {
	if original == nil || current == nil || original.Params.Origin == nil || current.Params.Origin == nil || current.Cursor < 0 || current.Cursor >= len(current.Items) || current.Items[current.Cursor] != selected {
		return false
	}
	a, b := original.Params.Origin, current.Params.Origin
	if a.Tool == "" || a.Tool != b.Tool || a.Command != b.Command || a.ScreenColumns != b.ScreenColumns || a.Warning != b.Warning || a.Explanation != b.Explanation || len(original.Params.Options) != len(current.Params.Options) {
		return false
	}
	found := false
	for i, option := range original.Params.Options {
		other := current.Params.Options[i]
		if option.ID != selected {
			if !reflect.DeepEqual(option, other) {
				return false
			}
			continue
		}
		if option.Role != "reject" || other.ID != selected || other.Role != "reject" {
			return false
		}
		found = true
		label := strings.TrimSpace(other.Label)
		if text == "" {
			if label != "No, and tell Claude what to do differently" {
				return false
			}
		} else {
			if !claudePermissionVisibleFeedback(screen, selected, text) {
				return false
			}
		}
	}
	return found
}

// Physical rows retain blank lines; a row boundary can also be a wrap. The
// transcript check confirms the actual newlines after the unchanged paste.
func claudePermissionVisibleFeedback(screen []string, selected, text string) bool {
	row := regexp.MustCompile(`^\s*[❯›>]\s*` + regexp.QuoteMeta(selected) + `[.)]\s+No, ?`)
	for i, line := range screen {
		where := row.FindStringIndex(line)
		if where == nil {
			continue
		}
		padding := strings.Repeat(" ", cellwidth.String(line[:where[1]]))
		rows := []string{line[where[1]:]}
		footer := false
		for _, next := range screen[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(next), "Esc to cancel") {
				footer = true
				break
			}
			if next == "" {
				rows = append(rows, "")
			} else if strings.HasPrefix(next, padding) {
				rows = append(rows, next[len(padding):])
			} else {
				return false
			}
		}
		if !footer {
			return false
		}
		if len(rows) > 1 && rows[len(rows)-1] == "" {
			rows = rows[:len(rows)-1]
		}
		offsets := map[int]bool{0: true}
		for n, value := range rows {
			nextOffsets := make(map[int]bool)
			for offset := range offsets {
				if !strings.HasPrefix(text[offset:], value) {
					continue
				}
				offset += len(value)
				nextOffsets[offset] = true
				if n < len(rows)-1 && offset < len(text) && (text[offset] == '\n' || text[offset] == ' ') {
					nextOffsets[offset+1] = true
				}
			}
			offsets = nextOffsets
		}
		return offsets[len(text)]
	}
	return false
}

// A numbered feedback line is not a new option. Inspect the selected text
// separately while retaining the surrounding permission screen for parsing.
func claudePermissionEditScreen(screen []string, selected string) []string {
	row := regexp.MustCompile(`^\s*[❯›>]\s*` + regexp.QuoteMeta(selected) + `[.)]\s+No, ?`)
	index, end := -1, 0
	for i, line := range screen {
		if where := row.FindStringIndex(line); where != nil {
			index, end = i, where[1]
		}
	}
	if index < 0 {
		return screen
	}
	indent := cellwidth.String(screen[index][:end])
	out := append([]string(nil), screen[:index]...)
	out = append(out, strings.TrimRight(screen[index][:end], " ")+" and tell Claude what to do differently")
	inFeedback := true
	for _, line := range screen[index+1:] {
		trim := strings.TrimLeft(line, " ")
		lineIndent := cellwidth.String(line) - cellwidth.String(trim)
		if lineIndent < indent && strings.HasPrefix(trim, "Esc to cancel") {
			inFeedback = false
		}
		if inFeedback && trim != "" && lineIndent >= indent {
			continue
		}
		out = append(out, line)
	}
	if inFeedback {
		return nil
	}
	return out
}

func claudePermissionEditPrompt(pane *runtime.Pane, selected string) *screenprompt.Prompt {
	if foregroundAgent(pane) != "claude" {
		return nil
	}
	prompt := screenprompt.Parse("claude", claudePermissionEditScreen(pane.DetectionLines(), selected))
	if prompt != nil && prompt.Params.Origin != nil && prompt.Params.Origin.Tool != "" {
		pane.WithEmu(func(e *vt.Emulator) { prompt.Params.Origin.ScreenColumns = e.Width() })
	}
	return prompt
}

func (s *Server) claudePermissionEditInput(pid int, original *screenprompt.Prompt, selected, expected, data string) error {
	err := fmt.Errorf("server stopped before permission input")
	s.app.Do(func() {
		pane, w := s.sup.Get(pid), s.screenPrompts.watches[pid]
		if pane == nil || pane.Snapshot().Dead || w == nil || s.screenUserActive(pid) || s.screenAnswerInterrupted(pid) || (!w.permissionDeadline.IsZero() && !time.Now().Before(w.permissionDeadline)) {
			err = fmt.Errorf("permission expired or interrupted")
			return
		}
		visible, _ := pane.ReadLines("visible", true)
		if !claudePermissionEditMatches(original, claudePermissionEditPrompt(pane, selected), selected, expected, visible) {
			err = fmt.Errorf("permission editor changed")
			return
		}
		sequence := w.answerInputSequence
		_, err = pane.WriteScreenPrompt([]byte(data), &sequence)
	})
	return err
}

func (s *Server) waitClaudePermissionEdit(pid int, original *screenprompt.Prompt, selected, text string) error {
	deadline := time.Now().Add(multiSelectSettle)
	for {
		var matches bool
		s.app.Do(func() {
			if pane := s.sup.Get(pid); pane != nil {
				visible, _ := pane.ReadLines("visible", true)
				matches = claudePermissionEditMatches(original, claudePermissionEditPrompt(pane, selected), selected, text, visible)
			}
		})
		if matches {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("permission editor was not verified")
		}
		timer := time.NewTimer(eventWaitPoll)
		select {
		case <-timer.C:
		case <-s.decisions.stopped:
			timer.Stop()
			return fmt.Errorf("server stopped during permission input")
		}
	}
}

func (s *Server) deliverClaudePermissionText(pid int, signature string, prompt *screenprompt.Prompt, answer api.DecisionAnswer) error {
	if answer.OptionID == nil || answer.Text == nil || *answer.Text == "" || strings.IndexFunc(*answer.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
		return fmt.Errorf("invalid permission feedback")
	}
	selected := *answer.OptionID
	reject := false
	for _, option := range prompt.Params.Options {
		if option.ID == selected && option.Role == "reject" {
			reject = true
		}
	}
	if !reject {
		return fmt.Errorf("feedback requires the No option")
	}
	var pane *runtime.Pane
	s.app.Do(func() { pane = s.sup.Get(pid) })
	if pane == nil {
		return fmt.Errorf("pane closed")
	}
	path, previous, err := claudePermissionPath(pane)
	if err != nil {
		return err
	}
	if err := s.navigateScreenRow(pid, signature, selected); err != nil {
		return err
	}
	var cursor int = -1
	for i, item := range prompt.Items {
		if item == selected {
			cursor = i
		}
	}
	if cursor < 0 {
		return fmt.Errorf("reject option unavailable")
	}
	if err := s.screenInputChecked(pid, signature, "\t", true, nil, &cursor); err != nil {
		return err
	}
	if err := s.waitClaudePermissionEdit(pid, prompt, selected, ""); err != nil {
		return err
	}
	var paste bool
	s.app.Do(func() {
		if pane := s.sup.Get(pid); pane != nil {
			paste = pane.BracketedPasteMode()
		}
	})
	text := *answer.Text
	if paste {
		text = "\x1b[200~" + text + "\x1b[201~"
	} else if strings.Contains(text, "\n") {
		return fmt.Errorf("cannot paste multiline feedback safely")
	}
	if err := s.claudePermissionEditInput(pid, prompt, selected, "", text); err != nil {
		return err
	}
	if err := s.waitClaudePermissionEdit(pid, prompt, selected, *answer.Text); err != nil {
		return err
	}
	if err := s.claudePermissionEditInput(pid, prompt, selected, *answer.Text, "\r"); err != nil {
		return err
	}
	deadline := time.Now().Add(claudeTranscriptWait)
	for {
		received, err := claudePermissionReceiptAny(path, *answer.Text, previous)
		if err != nil {
			return err
		}
		if received {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("Claude transcript did not confirm rejection feedback")
		}
		timer := time.NewTimer(eventWaitPoll)
		select {
		case <-timer.C:
		case <-s.decisions.stopped:
			timer.Stop()
			return fmt.Errorf("server stopped while verifying feedback")
		}
	}
}
