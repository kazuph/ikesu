package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestServerChromeRecordedControlsAndCopyStatus(t *testing.T) {
	s := testState(160, 45)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	out := Compose(Input{ServerUI: true, State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	text := screenText(t, out, s.Cols, s.Rows)
	for _, hidden := range []string{"[full]", "NORMAL", "[grouped]", "«"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("server shows an unobserved control %q", hidden)
		}
	}
	if out.NewTabRect.W != 0 || out.Frame.Cell(s.SidebarArea().W-1, 10).Content != " " {
		t.Fatal("server retains a hidden tab-add target or sidebar boundary")
	}
	copyOut := Compose(Input{ServerUI: true, State: s, Panes: map[int]*PaneSnap{pid: blankSnap()},
		Copy: &CopyMark{Pane: pid}, CopyHint: " COPY  line 1  y/enter copy", Copied: "Copied 1 lines"})
	copyText := screenText(t, copyOut, s.Cols, s.Rows)
	if !strings.HasPrefix(strings.Split(copyText, "\n")[s.Rows-1], " COPY ") || strings.Contains(copyText, "Copied 1") {
		t.Fatal("server copy status must cover the bottom row without local success feedback")
	}
}

func TestServerTabOverflowRetainsFirstLabelsWithoutArrows(t *testing.T) {
	s := testState(90, 25)
	for i := 2; i <= 18; i++ {
		s.Apply(model.CmdNewTab{Workspace: 0})
		w := s.ActiveWorkspace()
		w.TabRef().Name = fmt.Sprintf("TAB-%d", i)
	}
	out := Compose(Input{ServerUI: true, State: s, TabScroll: 100, TabScrollManual: true})
	row := strings.Split(screenText(t, out, s.Cols, s.Rows), "\n")[0]
	if !strings.Contains(row, " 1        TAB-2     TAB-3") || strings.ContainsAny(row, "<>…") || strings.Contains(row, "TAB-18") {
		t.Fatalf("server overflow must clip the first strip without exposing arrows: %q", row)
	}
	if out.TabScroll != 0 || out.TabLeftRect.W != 0 || out.TabRightRect.W != 0 || out.TabHits[17].W != 0 {
		t.Fatal("hidden server tabs must not retain scrolling or click targets")
	}
}
