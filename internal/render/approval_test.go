package render

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestApprovalOverlayAndPaneAnswerIndicator(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	pane := state.FocusedPane()
	out := Compose(Input{State: state, Panes: map[int]*PaneSnap{}, ApprovalCounts: map[int]int{pane: 1}, Approval: &Approval{Pane: pane, Title: "guard approval", Lines: []string{"command: echo hello", "request: test", "1 approve   2 day allow   3 reject"}}})
	var text strings.Builder
	for _, cell := range out.Frame.Cells {
		text.WriteString(cell.Content)
	}
	for _, want := range []string{"Answer (1)", "guard approval", "command: echo hello", "request: test"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("render missing %q", want)
		}
	}
	if out.ApprovalRect.W == 0 || out.AnswerRects[pane].W == 0 {
		t.Fatal("missing approval or answer hit area")
	}
}
