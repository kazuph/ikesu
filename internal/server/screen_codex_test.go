package server

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/screenprompt"
	"github.com/kazuph/goterrace/internal/teststate"
)

func codexQuestionTerminal(t *testing.T, fixture string) (*Server, *runtime.Pane, string) {
	t.Helper()
	s, p, input := screenTerminal(t, fixture, "codex")
	waitPromptCondition(t, func() bool {
		return screenprompt.Parse("codex", p.DetectionScreenLines()) != nil
	})
	return s, p, input
}

func TestCodexQuestionObservedCountdownKeepsDecisionID(t *testing.T) {
	teststate.Lock(t)
	s, p, input := codexQuestionTerminal(t, "codex-timed-first")
	scanScreen(s)
	d := screenRecord(t, s)
	if d.Body == nil || strings.ContainsAny(*d.Body, "0123456789") {
		t.Fatal("countdown body missing or contains numbers", d.Body)
	}
	for _, name := range []string{"codex-timed-countdown", "codex-timed-cursor", "codex-timed-free", "codex-timed-notes"} {
		paintScreen(p, screenDialog(t, name))
		scanScreen(s)
		got := screenRecord(t, s)
		if got.DecisionID != d.DecisionID || got.Title != d.Title || got.QuestionLabel == nil || *got.QuestionLabel != *d.QuestionLabel {
			t.Fatalf("%s changed saved question: %+v", name, got)
		}
	}
	if keys, err := os.ReadFile(input); err != nil || len(keys) != 0 {
		t.Fatalf("observation wrote PTY input: %q %v", keys, err)
	}
}

func TestCodexQuestionUnreadableRetainsPending(t *testing.T) {
	teststate.Lock(t)
	for _, fixture := range []string{"codex-question", "codex-question-notes"} {
		t.Run(fixture, func(t *testing.T) {
			s, p, input := codexQuestionTerminal(t, fixture)
			scanScreen(s)
			d := screenRecord(t, s)
			paintScreen(p, "  1. partial option row")
			for i := 0; i < 4; i++ {
				scanScreen(s)
			}
			got := screenRecord(t, s)
			if !reflect.DeepEqual(got, d) {
				t.Fatal(got)
			}
			s.app.Do(func() {
				if s.screenPrompts.watches[1].prompt.TextOpen != (fixture == "codex-question-notes") {
					t.Error("saved notes state was lost")
				}
			})
			resp := answerScreen(s, d, "1", nil)
			if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
				t.Fatal(resp)
			}
			if keys, _ := os.ReadFile(input); len(keys) != 0 {
				t.Fatalf("unverified question received %q", keys)
			}
			paintScreen(p, screenDialog(t, fixture))
			scanScreen(s)
			if screenRecord(t, s).DecisionID != d.DecisionID {
				t.Fatal("recovered question changed ID")
			}
		})
	}
}

// The saved screens come from a real Codex session. A raw PTY records the
// delivery bytes while its emulator advances through those observed states.
func TestCodexQuestionDelayedWritesStopAtChangedScreen(t *testing.T) {
	teststate.Lock(t)
	for _, stage := range []string{"before-text", "before-enter"} {
		for _, change := range []string{"other-question", "closed-notes", "unreadable"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				fixture := "codex-question-free"
				prefix := "\t"
				answer := "Custom acceptance answer"
				if stage == "before-enter" {
					fixture = "codex-question-notes"
					prefix = answer
				}
				s, p, input := codexQuestionTerminal(t, fixture)
				scanScreen(s)
				d := screenRecord(t, s)
				done := make(chan api.Response, 1)
				go func() { done <- answerScreen(s, d, "", &answer) }()
				waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == prefix })
				picture := screenDialog(t, "codex-question-notes")
				switch change {
				case "other-question":
					picture = strings.Replace(picture, d.Title, "A different question?", 1)
				case "closed-notes":
					picture = screenDialog(t, "codex-question-free")
				case "unreadable":
					picture = "  1. partial option row"
				}
				paintScreen(p, picture)
				select {
				case resp := <-done:
					if resp.Err == nil {
						t.Fatal("changed screen accepted remaining input")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("delivery did not stop")
				}
				keys, err := os.ReadFile(input)
				if err != nil || string(keys) != prefix {
					t.Fatalf("remaining input reached PTY: %q, want %q: %v", keys, prefix, err)
				}
				got, err := s.store.Decision(d.DecisionID)
				if err != nil || got.DecisionID != d.DecisionID {
					t.Fatal("decision identity lost", got, err)
				}
			})
		}
	}
}

func TestCodexQuestionFreeInputNavigationAndExistingNotes(t *testing.T) {
	teststate.Lock(t)
	for _, open := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "open"}[open], func(t *testing.T) {
			fixture := "codex-question"
			if open {
				fixture = "codex-question-notes"
			}
			s, p, input := codexQuestionTerminal(t, fixture)
			scanScreen(s)
			d := screenRecord(t, s)
			text := "Exact free answer"
			done := make(chan api.Response, 1)
			go func() { done <- answerScreen(s, d, "", &text) }()
			waitKeys := func(want string) {
				t.Helper()
				waitPromptCondition(t, func() bool { got, _ := os.ReadFile(input); return string(got) == want })
			}
			prefix := ""
			if !open {
				waitKeys("\x1b[B")
				paintScreen(p, screenDialog(t, "codex-question-cursor"))
				waitKeys("\x1b[B\x1b[B")
				paintScreen(p, screenDialog(t, "codex-question-free"))
				prefix = "\x1b[B\x1b[B\t"
				waitKeys(prefix)
				paintScreen(p, screenDialog(t, "codex-question-notes"))
			}
			select {
			case resp := <-done:
				if resp.Err != nil {
					t.Fatal(resp.Err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("free delivery did not finish")
			}
			waitKeys(prefix + text + "\r")
		})
	}
}
