package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"strings"
	"testing"
)

func TestHiddenIMECursorAllowListShapeAndReturn(t *testing.T) {
	in := Input{State: model.New("/tmp", 80, 24), Panes: map[int]*PaneSnap{1: {Agent: "claude", Cursor: CursorInfo{X: 3, Y: 2, Visible: false}}}}
	in.SidebarConfig.Experimental.RevealHiddenCursorForCJKIME = true
	for _, tc := range []struct {
		names   []string
		shape   string
		visible bool
		code    int
	}{
		{nil, "", true, 2}, {[]string{"claude"}, "bar", true, 5}, {[]string{"invalid"}, "bar", false, 0}, {[]string{"codex"}, "bar", false, 0},
	} {
		in.SidebarConfig.Experimental.CJKIMEAgents = tc.names
		in.SidebarConfig.Experimental.CJKIMECursorShape = tc.shape
		out := Compose(in)
		if out.AnchorVisible != tc.visible || out.AnchorShape != tc.code {
			t.Fatal(tc, out.AnchorVisible, out.AnchorShape)
		}
		if out.AnchorX != in.State.PanesArea().X+4 || out.AnchorY != in.State.PanesArea().Y+3 {
			t.Fatal("anchor drift")
		}
	}
	var enc Encoder
	out := Compose(in)
	out.AnchorShape = 2
	bytes := enc.Frame(out.Frame, out.AnchorX, out.AnchorY, true, out.AnchorShape)
	if !strings.Contains(string(bytes), "\x1b[2 q") {
		t.Fatal("shape missing")
	}
	bytes = enc.Frame(out.Frame, out.AnchorX, out.AnchorY, false, 0)
	if !strings.Contains(string(bytes), "\x1b[0 q") || !strings.Contains(string(bytes), hideCursor) {
		t.Fatal("cursor not restored")
	}
}
