package server

import (
	"os"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Replay the saved real question with the surrounding frame present in the
// live decision titles, then with the completed question still on screen.
func TestScreenAnswerInvestigation(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-single", "claude")
	picture := screenDialog(t, "claude-single")
	scanScreen(s)
	first := screenRecord(t, s)
	observed := screenDialog(t, "claude-left-border")
	plain := screenprompt.Parse("claude", strings.Split(observed, "\n"))
	bordered := strings.Replace(observed, "│ 検証用", "│\n│ 検証用", 1)
	boxed := screenprompt.Parse("claude", strings.Split(bordered, "\n"))
	t.Logf("frame blank row: plain=%q bordered=%q sameSignature=%t", plain.Params.Title, boxed.Params.Title, plain.Signature() == boxed.Signature())
	if response := answerScreen(s, first, "1", nil); response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { b, _ := os.ReadFile(input); return string(b) == "1" })
	paintScreen(p, "ordinary idle screen")
	scanScreen(s)
	scanScreen(s)
	paintScreen(p, picture+"\n⏺ Your answer was received.\n❯ next task\n")
	scanScreen(s)
	scanScreen(s)
	rows, err := s.store.Decisions("")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Logf("decision: id=%s status=%s title=%q", row.DecisionID, row.Status, row.Title)
	}
	t.Logf("completed screen replay decision count=%d", len(rows))
}
