package render

import (
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"strings"
	"testing"
)

func TestG5TaskTitleBranchZoomWorkspaceAndFooter(t *testing.T) {
	s := model.New("/tmp/repo", 160, 40)
	tab := s.ActiveWorkspace().TabRef()
	pid := tab.Layout.Focused()
	pane := &PaneSnap{Label: "codex", Agent: "codex", Title: "thinking", TaskTitle: "restore pane sessions", Branch: "feature"}
	in := Input{State: s, Panes: map[int]*PaneSnap{pid: pane}}
	for _, tc := range []struct {
		task, label string
		zoom        bool
		want        string
	}{
		{"", "codex", false, "%1 codex thinking feature"},
		{"restore pane sessions", "codex", false, "%1 codex restore pane sessions feature"},
		{"restore pane sessions", "reviewer", false, "%1 reviewer restore pane sessions feature"},
		{"restore pane sessions", "codex", true, "ZOOM %1 codex restore pane sessions feature"},
	} {
		pane.TaskTitle, pane.Label = tc.task, tc.label
		tab.Zoomed = 0
		if tc.zoom {
			tab.Zoomed = model.PaneID(pid)
		}
		text := screenText(t, Compose(in), 160, 40)
		if !strings.Contains(text, tc.want) {
			t.Fatal(tc, text)
		}
	}
	pane.TaskTitle = "restore pane sessions"
	if name := WorkspaceDisplayName(s.ActiveWorkspace(), in.Panes); name != "repo-restore pane sessions" {
		t.Fatal(name)
	}
	pane.Agent = ""
	if name := WorkspaceDisplayName(s.ActiveWorkspace(), in.Panes); name != "repo" {
		t.Fatal("shell title mixed in", name)
	}
	for _, label := range []string{" COPY ", " PHONE COPY ", " EXIT COPY "} {
		in.CopyLabel = label
		out := Compose(in)
		for _, tc := range []struct {
			r    layout.Rect
			want string
		}{
			{out.ActionBar.Copy, label}, {out.ActionBar.Cycle, " CYCLE LAYOUT "}, {out.ActionBar.Rotate, " ROTATE PANES "}, {out.ActionBar.Equalize, " EQUALIZE "},
		} {
			var row strings.Builder
			for x := tc.r.X; x < tc.r.X+tc.r.W; x++ {
				row.WriteString(out.Frame.Cell(x, tc.r.Y).Content)
			}
			if row.String() != tc.want {
				t.Fatal("footer", row.String(), tc.want)
			}
		}
	}
}

func TestG5TabMarkerPriorityWidthAndNameColumn(t *testing.T) {
	s := model.New("/tmp/repo", 160, 35)
	tab := s.ActiveWorkspace().TabRef()
	tab.Name = "日本語"
	tab.Layout.SplitFocused(layout.Vertical, 2)
	pane := map[int]*PaneSnap{1: {}, 2: {}}
	for _, tc := range []struct{ a, b, want string }{
		{"working", "blocked", "!"}, {"done", "working", ">"}, {"idle", "done", "+"}, {"unknown", "idle", " "}, {"idle", "idle", " "},
	} {
		pane[1].Status.Key, pane[2].Status.Key = tc.a, tc.b
		out := Compose(Input{State: s, Panes: pane})
		r := out.TabHits[0]
		if r.W != 12 || out.Frame.Cell(r.X+1, r.Y).Content != tc.want || out.Frame.Cell(r.X+2, r.Y).Content != "日" {
			t.Fatal(tc, r)
		}
	}
	tab.Name = "x"
	if r := Compose(Input{State: s}).TabHits[0]; r.W != 8 {
		t.Fatal("minimum", r)
	}
}

func TestG5DialogRealCursorUnicodeClampedAndSettingsFooter(t *testing.T) {
	s := model.New("/tmp/repo", 120, 40)
	for _, name := range []string{"Rename workspace", "New tab", "Rename tab", "Rename pane"} {
		for _, cursor := range []int{0, 1, 2, 3} {
			d := &Dialog{Title: name, Input: "a界b", InputEdit: true, InputCursor: cursor, Confirm: "rename", Cancel: "cancel"}
			out := Compose(Input{State: s, Dialog: d})
			r := out.DialogRect
			width := []int{0, 1, 3, 4}[cursor]
			if out.AnchorX != r.X+2+width || out.AnchorY != r.Y+3 || !out.AnchorVisible {
				t.Fatal(name, cursor, out.AnchorX, out.AnchorY)
			}
			enc := &Encoder{}
			emu := vt.NewEmulator(s.Cols, s.Rows)
			emu.Write(enc.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible))
			got := emu.CursorPosition()
			emu.Close()
			if got.X != out.AnchorX || got.Y != out.AnchorY {
				t.Fatal("real cursor", got)
			}
			if strings.Contains(screenText(t, out, s.Cols, s.Rows), "█") {
				t.Fatal("fake cursor")
			}
		}
	}
	f := NewFrame(40, 15)
	d := &Dialog{Title: "Rename pane", Input: strings.Repeat("界", 80), InputEdit: true, InputCursor: 80, Confirm: "rename", Cancel: "cancel"}
	out := &Output{}
	drawDialog(f, d, out)
	r := out.DialogRect
	if out.AnchorX != r.X+r.W-2 {
		t.Fatal("input right edge clamp", out.AnchorX, r)
	}
	for section := 0; section < 3; section++ {
		text := screenText(t, Compose(Input{State: s, Settings: &Settings{Section: section, Theme: "catppuccin"}}), s.Cols, s.Rows)
		if !strings.Contains(text, "↵ apply") || !strings.Contains(text, "esc close") || strings.Contains(text, "install") {
			t.Fatal(section, text)
		}
	}
}
