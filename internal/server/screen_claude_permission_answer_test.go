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
	if !claudePermissionEditMatches(original, current, "2", "feedback") {
		t.Fatal("second option rejection not recognized")
	}
	if claudePermissionEditMatches(original, current, "1", "feedback") {
		t.Fatal("approval accepted as rejection")
	}
}

func TestClaudePermissionEditMultiline(t *testing.T) {
	screen := "Bash command\nExplain\n╌╌╌╌\necho safe\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel · Tab to amend"
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	selected := strings.Replace(screen, "❯ 1. Yes", "  1. Yes", 1)
	selected = strings.Replace(selected, "  2. No", "❯ 2. No, Do not write the file.\n          Please explain the decision.", 1)
	current := screenprompt.Parse("claude", strings.Split(selected, "\n"))
	if !claudePermissionEditMatches(original, current, "2", "Do not write the file.\nPlease explain the decision.") {
		t.Fatal("multiline feedback not recognized")
	}
	if claudePermissionEditMatches(original, current, "2", "Do not write the file.\nDifferent text.") {
		t.Fatal("changed feedback accepted")
	}
	if claudePermissionEditMatches(original, current, "1", "Do not write the file.\nPlease explain the decision.") {
		t.Fatal("approval accepted as rejection")
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
	if !claudePermissionEditMatches(original, edit, "4", "") {
		t.Fatal("No editor not recognized")
	}
	if claudePermissionEditMatches(original, edit, "1", "") {
		t.Fatal("Yes editor accepted")
	}
	typed := strings.Replace(selected, "and tell Claude what to do differently", "Do not write files", 1)
	edit = screenprompt.Parse("claude", strings.Split(typed, "\n"))
	if !claudePermissionEditMatches(original, edit, "4", "Do not write files") {
		t.Fatal("feedback not recognized")
	}
	if claudePermissionEditMatches(original, edit, "4", "different text") {
		t.Fatal("changed feedback accepted")
	}
	changed := strings.Replace(typed, "printf observation", "printf different", 1)
	if claudePermissionEditMatches(original, screenprompt.Parse("claude", strings.Split(changed, "\n")), "4", "Do not write files") {
		t.Fatal("changed command accepted")
	}
}
