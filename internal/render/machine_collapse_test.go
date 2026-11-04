package render

import "testing"

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
