package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Feed the local VT emulator observed dialog states after actual PTY events.
// This proves the input protocol independently of a real Claude acceptance run.
func TestScreenMultiAnswerEventsToggleAndSubmitInLocalTerminal(t *testing.T) {
	teststate.Lock(t)
	for _, initiallyChecked := range []bool{false, true} {
		s, p, input := screenTerminal(t, "claude-multi-wide", "claude")
		picture := screenDialog(t, "claude-multi-wide")
		if initiallyChecked {
			picture = strings.Replace(picture, "[ ] Summary", "[✔] Summary", 1)
			paintScreen(p, picture)
		}
		scanScreen(s)
		d := screenRecord(t, s)
		ids := []string{"1", "2"}
		if initiallyChecked {
			ids = []string{"2"}
		}
		done := make(chan api.Response, 1)
		go func() {
			done <- s.dispatch(&api.Request{ID: "multi", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionIDs: ids})})
		}()
		waitBytes := func(want string) {
			t.Helper()
			waitPromptCondition(t, func() bool { got, _ := os.ReadFile(input); return string(got) == want })
		}
		waitBytes("1")
		first := time.Now()
		if initiallyChecked {
			picture = strings.Replace(picture, "[✔] Summary", "[ ] Summary", 1)
		} else {
			picture = strings.Replace(picture, "[ ] Summary", "[✔] Summary", 1)
		}
		paintScreen(p, picture)
		waitBytes("12")
		if time.Since(first) < agentPromptSubmitDelay-eventWaitPoll {
			t.Fatal("number keys were not separate delayed events")
		}
		picture = strings.Replace(picture, "[ ] Evidence", "[✔] Evidence", 1)
		picture = strings.Replace(picture, "❯ 1.", "  1.", 1)
		picture = strings.Replace(picture, "  2.", "❯ 2.", 1)
		paintScreen(p, picture)
		waitBytes("12\x1b[B")
		picture = strings.Replace(picture, "❯ 2.", "  2.", 1)
		picture = strings.Replace(picture, "  3.", "❯ 3.", 1)
		paintScreen(p, picture)
		waitBytes("12\x1b[B\x1b[B")
		picture = strings.Replace(picture, "❯ 3.", "  3.", 1)
		picture = strings.Replace(picture, "     Next", "❯    Next", 1)
		paintScreen(p, picture)
		select {
		case resp := <-done:
			if resp.Err != nil {
				t.Fatal(resp.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("multi answer did not finish")
		}
		waitBytes("12\x1b[B\x1b[B\r")
		got, err := s.store.Decision(d.DecisionID)
		if err != nil || got.Status != "answered" || got.Answer == nil || *got.Answer.OptionID != ids[0] {
			t.Fatal(got, err)
		}
	}
}

func TestScreenMultiInputFailureDoesNotSendSubmit(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-multi-wide", "claude")
	picture := screenDialog(t, "claude-multi-wide")
	picture = strings.Replace(picture, "[ ] Summary", "[✔] Summary", 1)
	picture = strings.Replace(picture, "[ ] Evidence", "[✔] Evidence", 1)
	paintScreen(p, picture)
	scanScreen(s)
	d := screenRecord(t, s)
	resp := s.dispatch(&api.Request{ID: "multi", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionIDs: []string{"1", "2"}})})
	if resp.Err == nil || resp.Err.Code != "screen_prompt_input_failed" {
		t.Fatal(resp)
	}
	waitPromptCondition(t, func() bool { got, _ := os.ReadFile(input); return len(got) > 0 })
	if got, _ := os.ReadFile(input); string(got) != "\x1b[B" {
		t.Fatalf("unexpected submit or retry: %q", got)
	}
}
