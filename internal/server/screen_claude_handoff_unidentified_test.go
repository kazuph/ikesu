package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestClaudeUnidentifiedQuestionAfterHandoff(t *testing.T) {
	teststate.Lock(t)
	for _, transition := range []string{"answered", "replaced"} {
		t.Run(transition, func(t *testing.T) {
			s := newTestServer(t, "handoff-unidentified-"+transition)
			root, cwd := t.TempDir(), t.TempDir()
			id := "ffffffff-ffff-ffff-ffff-ffffffffffff"
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
			picture := screenDialog(t, "claude-single")
			screen := filepath.Join(root, "screen.txt")
			if err = os.WriteFile(screen, []byte(picture), 0600); err != nil {
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
			callTime := time.Now().UTC()
			waitPromptCondition(t, func() bool { return time.Now().UnixMilli() > callTime.UnixMilli() })
			var old api.Decision
			s.app.Do(func() {
				p := s.readScreenPrompt(1)
				w := &screenPromptWatch{prompt: p, signature: p.Signature(), lastMatched: true}
				s.screenPrompts.watches = map[int]*screenPromptWatch{1: w}
				s.createScreenPrompt(1, w)
				var e error
				old, e = s.store.Decision(w.decisionID)
				if e != nil {
					t.Error(e)
				}
			})
			s.app.Do(func() { s.screenPrompts.watches = nil; s.screenPrompts.claudeRestore = nil })
			if transition == "answered" {
				paintScreen(pane, "ordinary idle screen")
				s.app.Do(func() { s.app.SetReported(1, "claude", "idle") })
			} else {
				paintScreen(pane, strings.ReplaceAll(picture, "Which output format should the temporary report use?", "A completely different question?"))
			}
			s.app.Do(func() { s.screenPromptConfig(config.Config{}) })
			options := []map[string]string{}
			for _, o := range old.Options {
				desc := ""
				if o.Description != nil {
					desc = *o.Description
				}
				options = append(options, map[string]string{"label": o.Label, "description": desc})
			}
			question := map[string]any{"timestamp": callTime.Format(time.RFC3339Nano), "sessionId": id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": "tool-before", "input": map[string]any{"questions": []any{map[string]any{"question": old.Title, "options": options}}}}}}}
			waitPromptCondition(t, func() bool { return time.Now().UnixMilli() > old.CreatedUnixMS })
			var later map[string]any
			if transition == "answered" {
				later = map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "sessionId": id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool-before"}}}}
			} else {
				later = map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "sessionId": id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": "tool-after", "input": map[string]any{"questions": []any{map[string]any{"question": "A completely different question?", "options": options}}}}}}}
			}
			a, _ := json.Marshal(question)
			b, _ := json.Marshal(later)
			data := append(append(append(a, '\n'), b...), '\n')
			if err := os.WriteFile(filepath.Join(projects, id+".jsonl"), data, 0600); err != nil {
				t.Fatal(err)
			}
			waitPromptCondition(t, func() bool {
				scanScreen(s)
				d, e := s.store.Decision(old.DecisionID)
				identity, _ := s.store.ScreenQuestionIdentity(old.DecisionID)
				return e == nil && d.Status == "cancelled" && identity != ""
			})
		})
	}
}
