package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

// This local emulator writes only to its temporary configuration root and uses
// the real PTY, process identity, JSONL files and terminal input path.
func TestClaudeTranscriptPTYEmulator(t *testing.T) {
	if os.Getenv("CLAUDE_PTY_EMULATOR") != "1" {
		return
	}
	root, id, output := os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("EMULATOR_SESSION"), os.Getenv("EMULATOR_INPUT")
	start, ok := platform.ProcStart(os.Getpid())
	if !ok {
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	record, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sessionId": id, "cwd": cwd, "startedAt": start.UnixMilli()})
	if err := os.WriteFile(filepath.Join(root, "sessions", fmt.Sprint(os.Getpid())+".json"), record, 0600); err != nil {
		os.Exit(2)
	}
	stty := exec.Command("stty", "raw", "-echo")
	stty.Stdin = os.Stdin
	if stty.Run() != nil {
		os.Exit(2)
	}
	picture, err := os.ReadFile(os.Getenv("EMULATOR_SCREEN"))
	if err != nil {
		os.Exit(2)
	}
	fmt.Print("\x1b[2J\x1b[H" + strings.ReplaceAll(string(picture), "\n", "\r\n"))
	f, err := os.Create(output)
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	var input [1]byte
	for {
		n, err := os.Stdin.Read(input[:])
		if n != 0 && input[0] == 4 {
			return
		}
		if n != 0 {
			_, _ = f.Write(input[:n])
		}
		if err != nil {
			return
		}
	}
}

func TestClaudeTranscriptRealPTYLateRecordAndSessionIsolation(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "claude-transcript-pty")
	root, cwd := t.TempDir(), t.TempDir()
	projects := filepath.Join(root, "projects", "same-cwd")
	if err := os.MkdirAll(projects, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "claude")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	screen := filepath.Join(root, "screen.txt")
	picture := strings.ReplaceAll(screenDialog(t, "claude-single"), " ☐ Format", "←  ☐ Sections  ☐ Language  ✔ Submit  →")
	if err := os.WriteFile(screen, []byte(picture), 0600); err != nil {
		t.Fatal(err)
	}
	ids := []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}
	startPane := func(pid int) (*runtime.Pane, string) {
		input := filepath.Join(root, fmt.Sprintf("input-%d", pid))
		env := []string{"CLAUDE_PTY_EMULATOR=1", "CLAUDE_CONFIG_DIR=" + root, "EMULATOR_SESSION=" + ids[pid-1], "EMULATOR_SCREEN=" + screen, "EMULATOR_INPUT=" + input}
		pane, err := s.sup.SpawnWith(pid, cwd, 160, 60, env, []string{link, "-test.run=^TestClaudeTranscriptPTYEmulator$"})
		if err != nil {
			t.Fatal(err)
		}
		pane.SetAgent("claude")
		waitPromptCondition(t, func() bool {
			return strings.Contains(pane.ScreenText(), "Enter to select") && foregroundAgent(pane) == "claude"
		})
		return pane, input
	}
	p1, input := startPane(1)
	p2, _ := startPane(2)
	s.app.SetReported(1, "claude", "blocked")
	s.initDecisionHub()
	s.app.SetQuitHook(s.stopDecisions)
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
		scanScreen(s)
		var ready bool
		s.app.Do(func() { w := s.screenPrompts.watches[1]; ready = w != nil && w.decisionID != "" })
		return ready
	})
	original := screenRecord(t, s)
	question := "画面に収まらない前提を含む全文。Which output format should the temporary report use?"
	writeQuestion := func(index int, text string) {
		record := map[string]any{"sessionId": ids[index], "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": "tool-" + ids[index], "input": map[string]any{"questions": []any{map[string]any{"question": text, "header": "Sections", "options": []any{map[string]any{"label": "Plain", "description": "A compact plain text report, easy to paste into terminals. 全文の説明"}, map[string]any{"label": "Markdown", "description": "A structured Markdown report with headings and links. もう一つの説明"}}}, map[string]any{"question": "全文。Which language should the report use?", "header": "Language", "options": []any{map[string]any{"label": "Japanese", "description": "For the local team. 日本語の説明"}, map[string]any{"label": "English", "description": "For wider sharing. 英語の説明"}}}}}}}}}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projects, ids[index]+".jsonl"), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeQuestion(0, question)
	writeQuestion(1, "別の会話の全文。Which output format should the temporary report use?")
	waitPromptCondition(t, func() bool { scanScreen(s); d := screenRecord(t, s); return d.Title == question })
	updated := screenRecord(t, s)
	if updated.DecisionID != original.DecisionID || updated.Options[0].Description == nil || *updated.Options[0].Description != "A compact plain text report, easy to paste into terminals. 全文の説明" {
		t.Fatalf("late update changed identity or lost description: %+v", updated)
	}
	p2screen := screenprompt.Parse("claude", p2.DetectionScreenLines())
	_, other, ok, _, _ := readClaudeTranscript(p2, p2screen, "claude", nil)
	if !ok || !strings.HasPrefix(other.Title, "別の会話の全文") {
		t.Fatalf("wrong session: %+v", other)
	}
	paintScreen(p1, strings.ReplaceAll(picture, "A compact plain text report, easy to paste into", "A dangerous alternative instead of the original"))
	if refused := answerScreen(s, updated, "1", nil); refused.Err == nil {
		t.Fatal("conflicting description accepted an answer")
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatalf("conflicting description received input: %q", keys)
	}
	paintScreen(p1, strings.ReplaceAll(picture, "Which output format should the temporary report use?", "Unrelated question?"))
	refused := answerScreen(s, updated, "1", nil)
	if refused.Err == nil {
		t.Fatal("unmatched screen accepted an answer")
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatalf("unmatched screen received input: %q", keys)
	}
	paintScreen(p1, strings.ReplaceAll(picture, "Which output format should the temporary report use?", "the temporary report use?"))
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var matched bool
		s.app.Do(func() {
			w := s.screenPrompts.watches[1]
			current := s.readScreenPrompt(1)
			matched = w != nil && current != nil && w.lastMatched && w.signature == current.Signature()
		})
		return matched
	})
	if got := screenRecord(t, s); got.DecisionID != original.DecisionID || got.Title != question {
		t.Fatalf("screen change rebuilt question: %+v", got)
	}
	response := answerScreen(s, updated, "1", nil)
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "1" })
	paintScreen(p1, screenDialog(t, "claude-second-tab"))
	var second api.Decision
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() {
			w := s.screenPrompts.watches[1]
			if w != nil && w.decisionID != "" && w.decisionID != original.DecisionID {
				second, _ = s.store.Decision(w.decisionID)
				ready = second.Title == "全文。Which language should the report use?"
			}
		})
		return ready
	})
	if second.Options[0].Description == nil || *second.Options[0].Description != "For the local team. 日本語の説明" {
		t.Fatalf("second question lost its own description: %+v", second)
	}
	if response := answerScreen(s, second, "1", nil); response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "11" })
	result := map[string]any{"sessionId": ids[0], "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool-" + ids[0]}}}}
	data, _ := json.Marshal(result)
	f, err := os.OpenFile(filepath.Join(projects, ids[0]+".jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(data, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var missing bool
		s.app.Do(func() { missing = s.screenPrompts.watches[1] == nil })
		return missing
	})
	for range 3 {
		scanScreen(s)
	}
	decisions, err := s.store.Decisions("pending")
	if err != nil || len(decisions) != 0 {
		t.Fatalf("answered record reappeared: %+v %v", decisions, err)
	}
	_, _ = p1.WriteToPTY([]byte{4})
	_, _ = p2.WriteToPTY([]byte{4})
	waitPromptCondition(t, func() bool { return p1.Snapshot().Dead && p2.Snapshot().Dead })
}
