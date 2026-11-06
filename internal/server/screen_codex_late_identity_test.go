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
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

// The local PTY emulator holds the actual rollout file open, as Codex does.
func TestCodexRolloutPTYEmulator(t *testing.T) {
	if os.Getenv("CODEX_PTY_EMULATOR") != "1" {
		return
	}
	f, err := os.Open(os.Getenv("EMULATOR_ROLLOUT"))
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
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
	var input [1]byte
	for {
		if _, err := os.Stdin.Read(input[:]); err != nil {
			return
		}
	}
}

func TestCodexLateIdentityReconcilesDuplicate(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "codex-late-duplicate")
	root := t.TempDir()
	rollout, screen := filepath.Join(root, "rollout.jsonl"), filepath.Join(root, "screen.txt")
	if err := os.WriteFile(rollout, []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"session\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(screen, []byte(screenDialog(t, "codex-question")), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	env := []string{"CODEX_PTY_EMULATOR=1", "EMULATOR_ROLLOUT=" + rollout, "EMULATOR_SCREEN=" + screen}
	pane, err := s.sup.SpawnWith(1, root, 160, 60, env, []string{link, "-test.run=^TestCodexRolloutPTYEmulator$"})
	if err != nil {
		t.Fatal(err)
	}
	pane.SetAgent("codex")
	s.app.SetReported(1, "codex", "blocked")
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
		return strings.Contains(pane.ScreenText(), "enter to submit answer") && foregroundAgent(pane) == "codex"
	})
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() {
			w := s.screenPrompts.watches[1]
			ready = w != nil && w.decisionID != "" && w.codexAnchorPath != ""
		})
		return ready
	})
	old := screenRecord(t, s)
	var bound api.Decision
	var params api.DecisionCreateParams
	s.app.Do(func() {
		params = s.readScreenPrompt(1).Params
		params.Origin = old.Origin
		var e error
		bound, e = s.store.CreateScreenDecision(params, time.Now(), "p1", `["session","call","q"]`)
		if e != nil {
			t.Error(e)
		}
	})
	options := []map[string]string{}
	for _, o := range params.Options {
		desc := ""
		if o.Description != nil {
			desc = *o.Description
		}
		options = append(options, map[string]string{"label": o.Label, "description": desc})
	}
	arguments, _ := json.Marshal(map[string]any{"questions": []any{map[string]any{"id": "q", "question": params.Title, "options": options}, map[string]any{"id": "q2", "question": "Scope?", "options": []any{map[string]string{"label": "Small"}}}}})
	record, _ := json.Marshal(map[string]any{"timestamp": time.UnixMilli(old.CreatedUnixMS - 1).UTC().Format(time.RFC3339Nano), "type": "response_item", "payload": map[string]any{"type": "function_call", "name": "request_user_input", "call_id": "call", "arguments": string(arguments)}})
	f, err := os.OpenFile(rollout, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(record, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	questions, err := session.ReadCodexQuestions(rollout, "session")
	if err != nil {
		t.Fatal(err)
	}
	var matches bool
	s.app.Do(func() { _, matches = chooseCodexQuestion(s.readScreenPrompt(1), questions) })
	if !matches {
		t.Fatalf("rollout does not match screen: %+v", questions)
	}
	waitPromptCondition(t, func() bool {
		scanScreen(s)
		var ready bool
		s.app.Do(func() { w := s.screenPrompts.watches[1]; ready = w != nil && w.decisionID == bound.DecisionID })
		return ready
	})
	got, err := s.store.Decision(old.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("old pending after late binding: %+v %v", got, err)
	}
}
