package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/platform"

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

func TestClaudeRestoreAnsweredWithoutVisibleScreen(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "claude-restore-answered")
	root, cwd := t.TempDir(), t.TempDir()
	id := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	projects := filepath.Join(root, "projects", "same-cwd")
	if err := os.MkdirAll(projects, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "claude")
	if err = os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	screen := filepath.Join(root, "screen.txt")
	if err = os.WriteFile(screen, []byte(screenDialog(t, "claude-single")), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{"CLAUDE_PTY_EMULATOR=1", "CLAUDE_CONFIG_DIR=" + root, "EMULATOR_SESSION=" + id, "EMULATOR_SCREEN=" + screen, "EMULATOR_INPUT=" + filepath.Join(root, "input")}
	pane, err := s.sup.SpawnWith(1, cwd, 160, 60, env, []string{link, "-test.run=^TestClaudeTranscriptPTYEmulator$"})
	if err != nil {
		t.Fatal(err)
	}
	pane.SetAgent("claude")
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
		return strings.Contains(pane.ScreenText(), "Enter to select") && foregroundAgent(pane) == "claude"
	})
	var answered, unknown, different api.Decision
	s.app.Do(func() {
		if _, err := s.db(); err != nil {
			t.Error(err)
			return
		}
		params := s.readScreenPrompt(1).Params
		params.Origin = &api.DecisionOrigin{PaneID: "p1", Agent: "claude", Reason: "screen_prompt", Machine: "local"}
		var e error
		answered, e = s.store.CreateScreenDecision(params, time.Now(), "p1", `["`+id+`","answered",0]`)
		if e != nil {
			t.Error(e)
			return
		}
		unknown, e = s.store.CreateDecision(params, time.Now())
		if e != nil {
			t.Error(e)
		}
		different, e = s.store.CreateScreenDecision(params, time.Now(), "p1", `["`+id+`","different",0]`)
		if e != nil {
			t.Error(e)
		}
		s.screenPrompts.watches = nil
	})
	question := map[string]any{"sessionId": id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": "answered", "input": map[string]any{"questions": []any{map[string]any{"question": answered.Title, "options": []any{map[string]any{"label": "Plain"}, map[string]any{"label": "Markdown"}}}}}}}}}
	response := map[string]any{"sessionId": id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "answered"}}}}
	a, _ := json.Marshal(question)
	b, _ := json.Marshal(response)
	data := append(append(a, '\n'), b...)
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(projects, id+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	paintScreen(pane, "ordinary idle screen")
	s.app.Do(func() { s.app.SetReported(1, "claude", "idle"); s.screenPromptConfig(config.Config{}) })
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		d, e := s.store.Decision(answered.DecisionID)
		return e == nil && d.Status == "cancelled"
	})
	got, err := s.store.Decision(unknown.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("unidentified question changed: %+v %v", got, err)
	}
	got, err = s.store.Decision(different.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("different identity changed: %+v %v", got, err)
	}
}

func TestClaudeTranscriptLateIdentityReconcilesDuplicate(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "claude-late-duplicate")
	root, cwd := t.TempDir(), t.TempDir()
	id := "cccccccc-cccc-cccc-cccc-cccccccccccc"
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
	if err := os.WriteFile(screen, []byte(screenDialog(t, "claude-single")), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{"CLAUDE_PTY_EMULATOR=1", "CLAUDE_CONFIG_DIR=" + root, "EMULATOR_SESSION=" + id, "EMULATOR_SCREEN=" + screen, "EMULATOR_INPUT=" + filepath.Join(root, "input")}
	pane, err := s.sup.SpawnWith(1, cwd, 160, 60, env, []string{link, "-test.run=^TestClaudeTranscriptPTYEmulator$"})
	if err != nil {
		t.Fatal(err)
	}
	pane.SetAgent("claude")
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
		return strings.Contains(pane.ScreenText(), "Enter to select") && foregroundAgent(pane) == "claude"
	})
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() { x := s.screenPrompts.transcripts[1]; ready = x != nil && x.latest.signature != "" })
		return ready
	})
	s.app.Do(func() { s.screenPrompts.watches[1].transcriptDeadline = time.Now().Add(-time.Second) })
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() { w := s.screenPrompts.watches[1]; ready = w != nil && w.decisionID != "" })
		return ready
	})
	old := screenRecord(t, s)
	identityBytes, _ := json.Marshal([]any{id, "tool-late", 0})
	var bound api.Decision
	s.app.Do(func() {
		params := s.readScreenPrompt(1).Params
		params.Origin = old.Origin
		var e error
		if e = s.store.BindScreenQuestion(old.DecisionID, "p1", `["`+id+`","other",0]`); e != nil {
			t.Error(e)
			return
		}
		bound, e = s.store.CreateScreenDecision(params, time.Now(), "p1", string(identityBytes))
		if e != nil {
			t.Error(e)
		}
	})
	record := map[string]any{"sessionId": id, "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": "tool-late", "input": map[string]any{"questions": []any{map[string]any{"question": old.Title, "header": "Format", "options": []any{map[string]any{"label": "Plain", "description": "A compact plain text report, easy to paste into terminals."}, map[string]any{"label": "Markdown", "description": "A structured Markdown report with headings and links."}}}}}}}}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, id+".jsonl"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() { w := s.screenPrompts.watches[1]; ready = w != nil && w.decisionID == bound.DecisionID })
		return ready
	})
	got, err := s.store.Decision(old.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("different question changed after transcript appeared: %+v %v", got, err)
	}
	s.app.Do(func() {
		p := s.readScreenPrompt(1)
		s.screenPrompts.watches[1] = &screenPromptWatch{prompt: p, signature: p.Signature(), decisionID: old.DecisionID, transcriptID: `["` + id + `","other",0]`, lastMatched: true}
	})
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() { w := s.screenPrompts.watches[1]; ready = w != nil && w.decisionID == bound.DecisionID })
		return ready
	})
	got, err = s.store.Decision(old.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("previous identified question changed: %+v %v", got, err)
	}
}
