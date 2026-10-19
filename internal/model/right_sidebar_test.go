package model

import "testing"

func TestRightSidebarNineGeometries(t *testing.T) {
	for left := 0; left < 3; left++ {
		for right := 0; right < 3; right++ {
			s := New("/tmp", 160, 30)
			for i := 0; i < left; i++ {
				s.Apply(CmdToggleSidebar{})
			}
			for i := 0; i < right; i++ {
				s.Apply(CmdToggleRightSidebar{})
			}
			widths := []int{26, 2, 0}
			l, r, p := s.SidebarArea(), s.RightSidebarArea(), s.PanesArea()
			if l.W != widths[left] || r.W != widths[right] || p.X != l.W || p.W != 160-l.W-r.W || r.X != p.X+p.W {
				t.Fatalf("%d/%d: left=%+v right=%+v panes=%+v", left, right, l, r, p)
			}
			if s.TabBarArea().W != p.W || s.ActionBarArea().W != p.W {
				t.Fatal("bars must share pane width")
			}
		}
	}
}

func TestRightSidebarAutomaticWidthDoesNotMutatePreference(t *testing.T) {
	s := New("/tmp", 100, 30)
	for _, tc := range []struct{ cols, w int }{{100, 26}, {80, 2}, {67, 0}, {100, 26}} {
		s.Cols = tc.cols
		if got := s.RightSidebarArea().W; got != tc.w {
			t.Fatalf("cols=%d right=%d want=%d", tc.cols, got, tc.w)
		}
		if s.RightSidebarMode != "" {
			t.Fatal("automatic adaptation was persisted")
		}
	}
	s.MobileWidthThreshold = 64
	s.Cols = 64
	if s.RightSidebarArea().W != 0 || s.SidebarArea().W != 0 || s.PanesArea().W != 64 {
		t.Fatal("mobile geometry changed")
	}
}

func TestLeftSidebarHiddenModeRemainsTwoStates(t *testing.T) {
	s := New("/tmp", 160, 30)
	s.SidebarCollapsedHidden = true
	for _, w := range []int{0, 26, 0, 26} {
		s.Apply(CmdToggleSidebar{})
		if s.SidebarArea().W != w {
			t.Fatal(s.SidebarArea())
		}
	}
}
