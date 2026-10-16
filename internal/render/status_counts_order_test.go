package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestMobileStatusCountsAttentionOrder(t *testing.T) {
	s := model.New("/tmp", 64, 25)
	s.MobileWidthThreshold = 64
	for i := 0; i < 3; i++ {
		s.Apply(model.CmdSplitFocused{Dir: 0})
	}
	in := Input{State: s, Panes: map[int]*PaneSnap{}}
	for i, key := range []string{"idle", "done", "working", "blocked"} {
		in.Panes[i+1] = &PaneSnap{Status: StatusGlyph{Key: key}}
	}
	f := NewFrame(64, 25)
	drawStatusLine(f, s, in)
	line := frameLine(f, 24)
	previous := -1
	for _, glyph := range []string{"▲", workingRune(0), "●", "○"} {
		pos := strings.Index(line, glyph)
		if pos <= previous {
			t.Fatalf("count order: %q", line)
		}
		previous = pos
	}
}
