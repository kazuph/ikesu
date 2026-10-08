package render

import (
	"strings"
	"testing"
)

func TestCopyHintClearsBorder(t *testing.T) {
	s := testState(100, 24)
	pid := s.Workspaces[0].Tabs[0].Layout.Focused()
	out := Compose(Input{
		State:    s,
		Panes:    map[int]*PaneSnap{pid: blankSnap()},
		Copy:     &CopyMark{Pane: pid, X: 0, Y: 0},
		CopyHint: " COPY  h/j/k/l test  q/esc exit",
	})
	text := screenText(t, out, 100, 24)
	found := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.Contains(ln, "q/esc exit") {
			found = true
			t.Logf("hint row: %q", ln)
			if strings.Contains(ln, "exit─") || strings.Contains(ln, "exit ┘") {
				t.Errorf("border remnant after hint: %q", ln)
			}
		}
	}
	if !found {
		t.Fatalf("hint row missing:\n%s", text)
	}
}
