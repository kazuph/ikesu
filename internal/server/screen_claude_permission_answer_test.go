package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestClaudePermissionEditInputStopsAfterInterruption(t *testing.T) {
	for _, stage := range []string{"after tab", "after paste"} {
		for _, cause := range []string{"expired", "user input", "screen changed", "pane closed"} {
			t.Run(stage+"/"+cause, func(t *testing.T) {
				teststate.Lock(t)
				s, pane, input := screenTerminal(t, "claude-permission", "claude")
				original := screenprompt.Parse("claude", strings.Split(screenDialog(t, "claude-permission"), "\n"))
				s.app.Do(func() {
					if s.screenPrompts.watches == nil {
						s.screenPrompts.watches = make(map[int]*screenPromptWatch)
					}
					s.screenPrompts.watches[1] = &screenPromptWatch{prompt: original, answerInputSequence: pane.ExternalInputSequence()}
				})
				switch cause {
				case "expired":
					s.app.Do(func() { s.screenPrompts.watches[1].permissionDeadline = time.Now().Add(-time.Second) })
				case "user input":
					pane.RecordExternalInput()
				case "screen changed":
					paintScreen(pane, "ordinary idle screen")
				case "pane closed":
					s.sup.Kill(1)
				}
				expected, next := "", "feedback"
				if stage == "after paste" {
					expected, next = "feedback", "\r"
				}
				if err := s.claudePermissionEditInput(1, original, "4", expected, next); err == nil {
					t.Fatal("interrupted input accepted")
				}
				keys, _ := os.ReadFile(input)
				if len(keys) != 0 {
					t.Fatalf("sent extra input: %q", keys)
				}
			})
		}
	}
}

func TestClaudePermissionChangedVisibleCommandSendsNoKeys(t *testing.T) {
	teststate.Lock(t)
	s, pane, input := screenTerminal(t, "claude-permission", "claude")
	picture := screenDialog(t, "claude-permission")
	picture = strings.Replace(picture, " printf observation >\n /tmp/gs-j0m1z8np/work/permission-observation.txt", " echo one\n echo two", 1)
	paintScreen(pane, picture)
	scanScreen(s)
	d := screenRecord(t, s)
	changed := strings.Replace(picture, " echo one\n echo two", " echo one echo two", 1)
	paintScreen(pane, changed)
	if response := answerScreen(s, d, "1", nil); response.Err == nil {
		t.Fatal("old answer was accepted")
	}
	keys, _ := os.ReadFile(input)
	if len(keys) != 0 {
		t.Fatalf("old answer sent input: %q", keys)
	}
}

func TestClaudePermissionEditorInterruptionsSendNoKeys(t *testing.T) {
	for _, stage := range []string{"after tab", "after paste"} {
		for _, cause := range []string{"expired", "user input", "screen changed", "pane closed"} {
			t.Run(stage+"/"+cause, func(t *testing.T) {
				teststate.Lock(t)
				s := newTestServer(t, "claude-permission-editor")
				root, cwd := t.TempDir(), t.TempDir()
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(t.TempDir(), "claude")
				if err := os.Symlink(executable, link); err != nil {
					t.Fatal(err)
				}
				originalScreen := screenDialog(t, "claude-permission")
				original := screenprompt.Parse("claude", strings.Split(originalScreen, "\n"))
				original.Params.Origin.ScreenColumns = 160
				editor := strings.Replace(originalScreen, "❯ 1. Yes", "  1. Yes", 1)
				expected := ""
				value := "and tell Claude what to do differently"
				if stage == "after paste" {
					expected, value = "feedback", "feedback"
				}
				editor = strings.Replace(editor, "  4. No", "❯ 4. No, "+value, 1)
				screen, input := filepath.Join(root, "screen"), filepath.Join(root, "keys")
				if err := os.WriteFile(screen, []byte(editor), 0600); err != nil {
					t.Fatal(err)
				}
				env := []string{"CLAUDE_PTY_EMULATOR=1", "CLAUDE_CONFIG_DIR=" + root, "EMULATOR_SESSION=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "EMULATOR_SCREEN=" + screen, "EMULATOR_INPUT=" + input}
				pane, err := s.sup.SpawnWith(1, cwd, 160, 60, env, []string{link, "-test.run=^TestClaudeTranscriptPTYEmulator$"})
				if err != nil {
					t.Fatal(err)
				}
				pane.SetAgent("claude")
				s.initDecisionHub()
				done := make(chan struct{})
				go func() { s.app.Run(); close(done) }()
				t.Cleanup(func() {
					s.app.Stop()
					<-done
					s.sup.KillAll()
					if s.store != nil {
						s.store.Close()
					}
				})
				waitPromptCondition(t, func() bool {
					return strings.Contains(pane.ScreenText(), "Esc to cancel") && foregroundAgent(pane) == "claude"
				})
				s.app.Do(func() {
					if s.screenPrompts.watches == nil {
						s.screenPrompts.watches = make(map[int]*screenPromptWatch)
					}
					s.screenPrompts.watches[1] = &screenPromptWatch{prompt: original, answerInputSequence: pane.ExternalInputSequence()}
				})
				if err := s.claudePermissionEditInput(1, original, "4", expected, ""); err != nil {
					t.Fatalf("unchanged editor must be accepted: %v", err)
				}
				switch cause {
				case "expired":
					s.app.Do(func() { s.screenPrompts.watches[1].permissionDeadline = time.Now().Add(-time.Second) })
				case "user input":
					pane.RecordExternalInput()
				case "screen changed":
					paintScreen(pane, strings.Replace(editor, "printf observation", "printf different", 1))
				case "pane closed":
					s.sup.Kill(1)
				}
				if err := s.claudePermissionEditInput(1, original, "4", expected, "x"); err == nil {
					t.Fatal("changed editor accepted")
				}
				keys, _ := os.ReadFile(input)
				if len(keys) != 0 {
					t.Fatalf("changed editor sent input: %q", keys)
				}
			})
		}
	}
}

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
	if ok, err := claudePermissionReceiptAny(path, "no", before); err != nil || ok {
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
	if ok, err := claudePermissionReceiptAny(path, "no", before); err != nil || !ok {
		t.Fatalf("rejection missing: %v %v", ok, err)
	}
	if ok, err := claudePermissionReceiptAny(path, "different", before); err != nil || ok {
		t.Fatalf("wrong feedback accepted: %v %v", ok, err)
	}
	if ok, _ := claudePermissionReceipt(path, "other", "no", before); ok {
		t.Fatal("wrong tool accepted")
	}
}

func TestClaudePermissionEditNarrowScreen(t *testing.T) {
	screen := "────────────────────────────────\n Bash command\n Check whether HOME and USER\n environment variables are set\n╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n for v in HOME USER; do if [ -n\n \"${(P)v}\" ]; then echo \"$v\n set\"; fi; done\n╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ Dangerous rm operation not\n │ ruled out — the shell may\n │ read this command\n │ differently than it was\n │ parsed\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend"
	original := screenprompt.Parse("claude", strings.Split(screen, "\n"))
	selected := strings.Replace(screen, " ❯ 1. Yes\n   2. No", "   1. Yes\n ❯ 2. No,and tell Claude what t…", 1)
	selected = strings.Replace(selected, "Esc to cancel · Tab to amend", "Esc to cancel", 1)
	rows := strings.Split(selected, "\n")
	current := screenprompt.Parse("claude", claudePermissionEditScreen(rows, "2"))
	if !claudePermissionEditMatches(original, current, "2", "", rows) {
		t.Fatalf("narrow No editor not recognized: original=%+v current=%+v", original, current)
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
