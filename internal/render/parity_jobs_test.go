package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"testing"
)

func TestParityJobsObservedRunningAndExitRow(t *testing.T) {
	exit := int64(7)
	success := int64(0)
	for _, tc := range []struct {
		status string
		exit   *int64
		want   string
		color  string
	}{
		{"running", nil, " run  p1       0s PARITY_", "yellow"},
		{"cancelled", nil, " done p1       0s PARITY_", "red"},
		{"exited", &success, " ok   p1       0s PARITY_", "green"},
		{"exited", &exit, " x7   p1       0s PARITY_", "red"},
	} {
		f := NewFrame(25, 1)
		col := Yellow
		if tc.color == "red" {
			col = Red
		} else if tc.color == "green" {
			col = Green
		}
		drawJobRow(f, Input{Jobs: []JobRow{{Label: "PARITY_JOB", Status: tc.status,
			CallerPane: "p1", Elapsed: "0s", ExitCode: tc.exit, Color: col}}}, 0, 0, 0, 25)
		got := ""
		for _, c := range f.Cells {
			got += c.Content
		}
		if got != tc.want {
			t.Fatalf("%s: %q want %q", tc.status, got, tc.want)
		}
		for x, want := range map[int]interface{}{1: f.ThemeColor(col), 6: f.Palette.Subtle,
			13: f.Palette.Dim, 18: f.Palette.Text} {
			if f.Cells[x].Style.Fg != want || f.Cells[x].Style.Bg != f.Palette.SurfDim {
				t.Fatalf("%s cell %d: wrong recorded style", tc.status, x)
			}
		}
	}
}

func TestServerJobDotAndRestoreStyleRecordedCells(t *testing.T) {
	s := testState(160, 45)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	out := Compose(Input{ServerUI: true, State: s, Panes: map[int]*PaneSnap{pid: {Agent: "codex", Restore: "saved session"}}, JobDots: map[int][]JobDot{0: {{Color: Yellow}}}})
	if out.Frame.Cell(3, 5).Content != "●" || out.Frame.Cell(4, 5).Style.Fg != out.Frame.Palette.Text || out.Frame.Cell(5, 5).Content != "n" || out.Frame.Cell(24, 4).Content != " " {
		t.Fatal("server job dot must precede git with a plain trailing space, not trail the name")
	}
	for x := 144; x < 159; x++ {
		if out.Frame.Cell(x, 1).Style.Attrs&uv.AttrBold == 0 {
			t.Fatal("focused saved-session label must be bold", x)
		}
	}
}
