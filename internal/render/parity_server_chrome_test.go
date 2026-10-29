package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestServerChromeRecordedControlsAndCopyStatus(t *testing.T) {
	s := testState(160, 45)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	out := Compose(Input{ServerUI: true, State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	text := screenText(t, out, s.Cols, s.Rows)
	for _, hidden := range []string{"«"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("server shows an unobserved control %q", hidden)
		}
	}
	if !strings.Contains(strings.Split(text, "\n")[0], "[full]") {
		t.Fatal("server density button missing")
	}
	if out.NewTabRect.W != 3 || !strings.Contains(text, " NORMAL ") || out.Frame.Cell(s.SidebarArea().W-1, 10).Content != "│" {
		t.Fatal("server lost tab-add target or sidebar boundary")
	}
	copyOut := Compose(Input{ServerUI: true, State: s, Panes: map[int]*PaneSnap{pid: blankSnap()},
		Copy: &CopyMark{Pane: pid}, CopyHint: " COPY  line 1  y/enter copy", Copied: "Copied 1 line"})
	copyText := screenText(t, copyOut, s.Cols, s.Rows)
	if !strings.Contains(strings.Split(copyText, "\n")[s.Rows-1], " COPY ") || !strings.Contains(copyText, "Copied 1 line") {
		t.Fatal("server copy status and sidebar success feedback must both remain visible", copyText)
	}
}

func TestServerTabOverflowUsesScrollableChipsAndArrows(t *testing.T) {
	s := testState(90, 25)
	for i := 2; i <= 18; i++ {
		s.Apply(model.CmdNewTab{Workspace: 0})
		w := s.ActiveWorkspace()
		w.TabRef().Name = fmt.Sprintf("TAB-%d", i)
	}
	out := Compose(Input{ServerUI: true, State: s, TabScroll: 100, TabScrollManual: true})
	row := strings.Split(screenText(t, out, s.Cols, s.Rows), "\n")[0]
	if !strings.ContainsAny(row, "<>…") || !strings.Contains(row, " +") {
		t.Fatalf("server overflow must expose measured arrows and add button: %q", row)
	}
	if out.TabScroll == 0 || out.TabLeftRect.W != 3 || out.TabRightRect.W != 3 || out.NewTabRect.W != 3 {
		t.Fatal("server overflow controls must have real click targets")
	}
}
