package server

import (
	"os"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Replay observed screens after a question is answered in the terminal or
// auto-resolves. No decision API answer is issued for the old question.
func TestCodexQuestionTransitionsToPermission(t *testing.T) {
	teststate.Lock(t)
	for _, fixture := range []string{"codex-question", "codex-question-notes"} {
		t.Run(fixture, func(t *testing.T) {
			s, p, input := codexQuestionTerminal(t, fixture)
			scanScreen(s)
			old := screenRecord(t, s)
			assertPending := func() {
				t.Helper()
				records, err := s.store.Decisions("pending")
				if err != nil || len(records) != 1 || records[0].DecisionID != old.DecisionID {
					t.Fatalf("old question not retained: %+v %v", records, err)
				}
			}
			question, permission := screenDialog(t, fixture), screenDialog(t, "codex-permission")
			// A single different screen must not cancel, and a matching read resets it.
			paintScreen(p, permission)
			scanScreen(s)
			assertPending()
			paintScreen(p, question)
			scanScreen(s)
			assertPending()
			// A missing read keeps the question and interrupts consecutive mismatches.
			paintScreen(p, permission)
			scanScreen(s)
			assertPending()
			paintScreen(p, "  1. partial option row")
			scanScreen(s)
			assertPending()
			paintScreen(p, permission)
			scanScreen(s)
			assertPending()
			scanScreen(s)
			next := screenRecord(t, s)
			got, err := s.store.Decision(old.DecisionID)
			if err != nil || got.Status != "cancelled" || next.DecisionID == old.DecisionID || next.Kind != "tool_permission" {
				t.Fatalf("transition: old=%+v next=%+v err=%v", got, next, err)
			}
			records, err := s.store.Decisions("pending")
			if err != nil || len(records) != 1 || records[0].DecisionID != next.DecisionID {
				t.Fatalf("duplicate or missing permission: %+v %v", records, err)
			}
			all, err := s.store.Decisions("")
			if err != nil || len(all) != 2 {
				t.Fatalf("unexpected history: %+v %v", all, err)
			}
			if keys, _ := os.ReadFile(input); len(keys) != 0 {
				t.Fatalf("watch transition wrote input: %q", keys)
			}
			if resp := answerScreen(s, next, "3", nil); resp.Err != nil {
				t.Fatal(resp.Err)
			}
			waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\x1b" })
		})
	}
}

func TestCodexQuestionChangedScreenRejectsOldAnswer(t *testing.T) {
	teststate.Lock(t)
	for _, target := range []string{"permission", "next-question"} {
		t.Run(target, func(t *testing.T) {
			s, p, input := codexQuestionTerminal(t, "codex-question")
			scanScreen(s)
			old := screenRecord(t, s)
			picture := screenDialog(t, "codex-permission")
			if target == "next-question" {
				picture = strings.Replace(screenDialog(t, "codex-question"), old.Title, "A different question?", 1)
			}
			paintScreen(p, picture)
			resp := answerScreen(s, old, "1", nil)
			if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
				t.Fatal(resp)
			}
			if keys, _ := os.ReadFile(input); len(keys) != 0 {
				t.Fatalf("stale answer wrote input: %q", keys)
			}
		})
	}
}
