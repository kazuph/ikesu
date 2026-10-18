package render

import "testing"

func TestStage6aFixWorktreeCountGeometryAndClicks(t *testing.T) {
	for _, tt := range []struct{ count, height int }{{1, 12}, {2, 12}, {3, 13}, {13, 26}} {
		m := &Menu{Worktree: true, Total: tt.count, Sel: tt.count - 1}
		for i := 0; i < tt.count; i++ {
			m.Items = append(m.Items, MenuItem{Label: "checkout", Detail: "/isolated/checkout"})
		}
		f := NewFrame(160, 45)
		out := &Output{}
		drawWorktreeMenu(f, m, out)
		if out.MenuRect.H != tt.height {
			t.Fatalf("count=%d rect=%+v", tt.count, out.MenuRect)
		}
		last := out.MenuItemRows[tt.count-1]
		if last.H != 2 || WorktreeMenuItemAt(m, 160, 45, last.X, last.Y) != tt.count-1 {
			t.Fatalf("last checkout not visible/clickable: count=%d rect=%+v", tt.count, last)
		}
		m.Items = m.Items[:1]
		m.Sel = 0
		filtered := MenuRect(m, 160, 45)
		if filtered != out.MenuRect {
			t.Fatalf("filter resized %v -> %v", out.MenuRect, filtered)
		}
	}
}
