package render

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/machine"
)

// At every sidebar width the remote machine header that keeps a click target
// also shows its ▾/▸ in the composed frame, including the one-column case.
func TestMachineHeaderGlyphVisibleAtEveryWidth(t *testing.T) {
	in := continuousInput(t, true, false)
	for _, collapsed := range []bool{false, true} {
		want := "▾"
		if collapsed {
			want = "▸"
		}
		m := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "LOCAL", Input: &in}, {ID: "remote", Label: "REMOTE", Input: &in}}, Active: "local", Collapsed: map[string]bool{"remote": collapsed}}
		for _, w := range []int{26, 18, 10, 5, 3, 2} {
			continuousWidth(in, w)
			f := ComposeMachines(in, &m).Frame
			found := false
			for _, hit := range m.Hits {
				if hit.Right || !hit.Header || hit.Machine != "remote" {
					continue
				}
				found = true
				if got := f.Cell(hit.Rect.X, hit.Rect.Y).Content; got != want {
					t.Fatalf("collapsed=%t width=%d header W=%d shows %q, want %q", collapsed, w, hit.Rect.W, got, want)
				}
			}
			if !found {
				t.Fatalf("collapsed=%t width=%d remote header target missing", collapsed, w)
			}
		}
	}
}

func TestMachineHeaderButtonMatchesConnectionAndWidth(t *testing.T) {
	t.Parallel()
	in := continuousInput(t, true, false)
	m := MachineSidebar{Groups: []MachineGroup{
		{ID: "local", Label: "LOCAL", Input: &in},
		{ID: "remote", Label: "REMOTE"},
	}, Active: "local"}
	for _, width := range []int{26, 18, 17, 14, 13, 11, 10, 7, 6, 2} {
		continuousWidth(in, width)
		for _, tc := range []struct {
			status, button string
			snapshot       *Input
		}{
			{"disconnected", "[connect]", nil},
			{"connecting", "[disconnect]", nil},
			{"online", "[disconnect]", nil},
			{"online", "[new]", &in},
			{"reconnecting", "[disconnect]", &in},
			{"needs attention", "[disconnect]", &in},
			{"awaiting approval", "[disconnect]", nil},
			{machine.ApprovalStopped, "[disconnect]", nil},
			{"disconnected", "[connect]", nil},
		} {
			remote := &m.Groups[1]
			remote.Status, remote.Input = tc.status, tc.snapshot
			frame := ComposeMachines(in, &m).Frame
			var header, button layout.Rect
			for _, hit := range m.Hits {
				if hit.Machine != remote.ID || hit.Right {
					continue
				}
				if hit.Header {
					header = hit.Rect
				}
				if hit.IsHeaderButton() {
					if button.W != 0 || hit.New != (tc.button == "[new]") ||
						hit.Connect != (tc.button == "[connect]") || hit.Disconnect != (tc.button == "[disconnect]") {
						t.Fatalf("width=%d status=%q unexpected button hit %+v", width, tc.status, hit)
					}
					button = hit.Rect
				}
			}
			if header.W != width-1 {
				t.Fatalf("width=%d status=%q unexpected header %+v", width, tc.status, header)
			}
			var label strings.Builder
			for x := header.X; x < header.X+header.W; x++ {
				label.WriteString(frame.Cell(x, header.Y).Content)
			}
			if header.W <= len(tc.button) {
				if button.W != 0 || strings.ContainsAny(label.String(), "[]") {
					t.Fatalf("width=%d status=%q shows a button that doesn't fit: %q, hit=%+v", width, tc.status, label.String(), button)
				}
				continue
			}
			want := layout.Rect{X: header.X + header.W - len(tc.button), Y: header.Y, W: len(tc.button), H: 1}
			if button != want || !strings.HasSuffix(label.String(), tc.button) {
				t.Fatalf("width=%d status=%q header=%q hit=%+v, want %q at %+v", width, tc.status, label.String(), button, tc.button, want)
			}
		}
	}
}
