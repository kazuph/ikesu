package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestScreenMultiTextNavigatesFreeRowAndPreservesChecks(t *testing.T) {
	testScreenMultiText(t, true)
}

func TestScreenMultiTextSelectsCommittedFreeRow(t *testing.T) {
	testScreenMultiText(t, false)
}

func testScreenMultiText(t *testing.T, checksOnEnter bool) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-multi-text", "claude")
	picture := screenDialog(t, "claude-multi-text")
	scanScreen(s)
	d := screenRecord(t, s)
	text := "Risks section."
	done := make(chan api.Response, 1)
	go func() {
		done <- s.dispatch(&api.Request{ID: "text", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: strPtr("1"), Text: &text})})
	}()
	waitBytes := func(want string) {
		t.Helper()
		waitPromptCondition(t, func() bool { got, _ := os.ReadFile(input); return string(got) == want })
	}
	waitBytes("1")
	picture = strings.Replace(picture, "[ ] Summary", "[✔] Summary", 1)
	paintScreen(p, picture)
	waitBytes("1\x1b[B")
	picture = strings.Replace(picture, "❯ 1.", "  1.", 1)
	picture = strings.Replace(picture, "  2.", "❯ 2.", 1)
	paintScreen(p, picture)
	waitBytes("1\x1b[B\x1b[B")
	picture = strings.Replace(picture, "❯ 2.", "  2.", 1)
	picture = strings.Replace(picture, "  3.", "❯ 3.", 1)
	paintScreen(p, picture)
	prefix := "1\x1b[B\x1b[B" + text
	waitBytes(prefix)
	picture = strings.Replace(picture, "[ ] Type something", "[ ] "+text, 1)
	paintScreen(p, picture)
	waitBytes(prefix + "\r")
	if checksOnEnter {
		picture = strings.Replace(picture, "[ ] "+text, "[✔] "+text, 1)
	} else {
		waitBytes(prefix + "\r\r")
		picture = strings.Replace(picture, "[ ] "+text, "[✔] "+text, 1)
		prefix += "\r"
	}
	paintScreen(p, picture)
	waitBytes(prefix + "\r\x1b[B")
	picture = strings.Replace(picture, "❯ 3.", "  3.", 1)
	picture = strings.Replace(picture, "     Submit", "❯    Submit", 1)
	paintScreen(p, picture)
	prefix += "\r\x1b[B"
	select {
	case resp := <-done:
		if resp.Err != nil {
			t.Fatal(resp.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("free input did not complete")
	}
	waitBytes(prefix + "\r")
	s.app.Do(func() {
		if s.screenPrompts.watches[1].editingText {
			t.Error("free-row override outlived input delivery")
		}
	})
}
