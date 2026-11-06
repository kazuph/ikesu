package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/screenprompt"
)

func TestClaudePermissionReceiptRejectOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"user","userFeedback":"no","permissionDecision":{"decision":"accept"},"message":{"content":[{"type":"tool_result","tool_use_id":"call1","is_error":false}]}}` + "\n")
	if err == nil {
		_, err = f.WriteString(`{"type":"user","toolDenialKind":"user-rejected","userFeedback":"no","permissionDecision":{"decision":"accept"},"message":{"content":[{"type":"tool_result","tool_use_id":"call1","is_error":true}]}}` + "\n")
	}
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if ok, err := claudePermissionReceipt(path, "call1", "no", before); err != nil || ok {
		t.Fatalf("accepted as rejection: %v %v", ok, err)
	}
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"user","toolDenialKind":"user-rejected","userFeedback":"no","message":{"content":[{"type":"tool_result","tool_use_id":"call1","is_error":true}]}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if ok, err := claudePermissionReceipt(path, "call1", "no", before); err != nil || !ok {
		t.Fatalf("rejection missing: %v %v", ok, err)
	}
	if ok, _ := claudePermissionReceipt(path, "other", "no", before); ok {
		t.Fatal("wrong tool accepted")
	}
}

func TestClaudePermissionEditNoSecondOption(t *testing.T) {
	screen := "Bash command\nExplain\n╌╌╌╌\necho safe\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel · Tab to amend"
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	selected := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	selected = strings.Replace(selected, "  2. No", "❯ 2. No, feedback", 1)
	current := screenprompt.Parse("claude", strings.Split(selected, "\n"))
	if !claudePermissionEditMatches(original, current, "2", "feedback", strings.Split(selected, "\n")) {
		t.Fatal("second option rejection not recognized")
	}
	if claudePermissionEditMatches(original, current, "1", "feedback", strings.Split(selected, "\n")) {
		t.Fatal("approval accepted as rejection")
	}
}

func TestClaudePermissionEditMultiline(t *testing.T) {
	screen := "Bash command\nExplain\n╌╌╌╌\necho safe\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel · Tab to amend"
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	selected := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	selected = strings.Replace(selected, "  2. No", " ❯ 2. No, Do not write the file.\n          Please explain the decision.", 1)
	current := screenprompt.Parse("claude", strings.Split(selected, "\n"))
	if !claudePermissionEditMatches(original, current, "2", "Do not write the file.\nPlease explain the decision.", strings.Split(selected, "\n")) {
		t.Fatal("multiline feedback not recognized")
	}
	if claudePermissionEditMatches(original, current, "2", "Do not write the file.\nDifferent text.", strings.Split(selected, "\n")) {
		t.Fatal("changed feedback accepted")
	}
	if claudePermissionEditMatches(original, current, "1", "Do not write the file.\nPlease explain the decision.", strings.Split(selected, "\n")) {
		t.Fatal("approval accepted as rejection")
	}
	wrapped := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	wrapped = strings.Replace(wrapped, "  2. No", " ❯ 2. No, Please do not run this command because this explanation is deliberately longer than a single\n          line on the current terminal screen.\n          Please explain the decision.", 1)
	want := "Please do not run this command because this explanation is deliberately longer than a single line on the current terminal screen.\nPlease explain the decision."
	if !claudePermissionEditMatches(original, screenprompt.Parse("claude", strings.Split(wrapped, "\n")), "2", want, strings.Split(wrapped, "\n")) {
		t.Fatal("wrapped multiline feedback not recognized")
	}
}

func TestClaudePermissionVisibleFeedback(t *testing.T) {
	tests := []struct {
		rows []string
		text string
		want bool
	}{
		{[]string{" ❯ 4. No, First line.", "          Second line.", "", " Esc to cancel"}, "First line.\nSecond line.", true},
		{[]string{" ❯ 4. No, First line.", "", "          Third line.", "", " Esc to cancel"}, "First line.\n\nThird line.", true},
		{[]string{" ❯ 4. No, 長い文章", "          続きます", "          次の行", "", " Esc to cancel"}, "長い文章続きます\n次の行", true},
		{[]string{" ❯ 4. No, First line.", "          Changed.", "", " Esc to cancel"}, "First line.\nSecond line.", false},
		{[]string{"   4. No, First line.", "          Second line.", "", " Esc to cancel"}, "First line.\nSecond line.", false},
		{[]string{" ❯ 4. No, First line.", "          Second line."}, "First line.\nSecond line.", false},
	}
	for _, tt := range tests {
		if got := claudePermissionVisibleFeedback(tt.rows, "4", tt.text); got != tt.want {
			t.Errorf("rows=%q text=%q: got %v want %v", tt.rows, tt.text, got, tt.want)
		}
	}
}

func TestClaudePermissionEditNumberedFeedback(t *testing.T) {
	screen := screenDialog(t, "claude-permission")
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	selected := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	selected = strings.Replace(selected, "   4. No", " ❯ 4. No, Please do not write the file.\n          1. Explain the reason.", 1)
	rows := strings.Split(selected, "\n")
	current := screenprompt.Parse("claude", claudePermissionEditScreen(rows, "4"))
	if !claudePermissionEditMatches(original, current, "4", "Please do not write the file.\n1. Explain the reason.", rows) {
		t.Fatal("numbered feedback was mistaken for another option")
	}
	if claudePermissionEditMatches(original, current, "4", "Please do not write the file.\n1. Different reason.", rows) {
		t.Fatal("changed feedback accepted")
	}
}

func TestClaudePermissionEditRequiresSelectedNo(t *testing.T) {
	screen := screenDialog(t, "claude-permission")
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	if original == nil {
		t.Fatal("permission not parsed")
	}
	selected := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	selected = strings.Replace(selected, "  4. No", "❯ 4. No, and tell Claude what to do differently", 1)
	edit := screenprompt.Parse("claude", strings.Split(selected, "\n"))
	if !claudePermissionEditMatches(original, edit, "4", "", strings.Split(selected, "\n")) {
		t.Fatal("No editor not recognized")
	}
	if claudePermissionEditMatches(original, edit, "1", "", strings.Split(selected, "\n")) {
		t.Fatal("Yes editor accepted")
	}
	typed := strings.Replace(selected, "and tell Claude what to do differently", "Do not write files", 1)
	edit = screenprompt.Parse("claude", strings.Split(typed, "\n"))
	if !claudePermissionEditMatches(original, edit, "4", "Do not write files", strings.Split(typed, "\n")) {
		t.Fatal("feedback not recognized")
	}
	if claudePermissionEditMatches(original, edit, "4", "different text", strings.Split(typed, "\n")) {
		t.Fatal("changed feedback accepted")
	}
	changed := strings.Replace(typed, "printf observation", "printf different", 1)
	if claudePermissionEditMatches(original, screenprompt.Parse("claude", strings.Split(changed, "\n")), "4", "Do not write files", strings.Split(changed, "\n")) {
		t.Fatal("changed command accepted")
	}
}
