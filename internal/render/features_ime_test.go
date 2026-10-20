package render

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
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
	enc.Frame(out.Frame, out.AnchorX, out.AnchorY, true, 5)
	if string(enc.RestoreCursor()) != "\x1b[0 q" || len(enc.RestoreCursor()) != 0 {
		t.Fatal("exit did not restore only the overridden cursor")
	}
}

func TestIMEOverrideFollowsPopupCursorAndClearsForNameInput(t *testing.T) {
	s := model.New("/tmp", 80, 24)
	s.Popup = &model.PopupPaneState{WorkspaceID: s.ActiveWorkspace().ID, PaneID: 2, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}}
	in := Input{State: s, Panes: map[int]*PaneSnap{2: {Agent: "claude", Cursor: CursorInfo{Visible: false}}}}
	in.SidebarConfig.Experimental.RevealHiddenCursorForCJKIME = true
	in.SidebarConfig.Experimental.CJKIMECursorShape = "bar"
	f := NewFrame(80, 24)
	out := Output{AnchorShape: 2}
	drawPopup(f, in, &out)
	if !out.AnchorVisible || out.AnchorShape != 5 {
		t.Fatal(out)
	}
	in.Panes[2].Cursor.Visible = true
	drawPopup(f, in, &out)
	if !out.AnchorVisible || out.AnchorShape != 0 {
		t.Fatal(out)
	}
	out.AnchorShape = 5
	drawNameInput(f, &Dialog{InputEdit: true}, &out)
	if out.AnchorShape != 0 {
		t.Fatal("pane override leaked into name entry")
	}
}
