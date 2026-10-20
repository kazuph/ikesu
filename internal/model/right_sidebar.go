package model

// The display preference is independent of temporary adaptation to a narrow terminal.
const MinimumPaneAreaWidth = 40

type CmdToggleRightSidebar struct{}

func (CmdToggleRightSidebar) isCommand() {}

func (s *AppState) ToggleSidebar() {
	if s.SidebarVisible {
		s.SidebarVisible = false
		s.SidebarHidden = false
	} else if s.SidebarCollapsedHidden || s.SidebarHidden {
		s.SidebarVisible = true
		s.SidebarSize = s.ExpandedSidebarWidth()
		s.SidebarHidden = false
	} else {
		s.SidebarHidden = true
	}
}
func (s *AppState) ToggleRightSidebar() {
	switch s.RightSidebarMode {
	case "":
		s.RightSidebarMode = "compact"
	case "compact":
		s.RightSidebarMode = "hidden"
	default:
		s.RightSidebarMode = ""
		s.RightSidebarSize = s.ExpandedRightSidebarWidth()
	}
}
func (s *AppState) ExpandedRightSidebarWidth() int {
	if s.RightSidebarSize <= 0 {
		_, _, normal := s.SidebarBounds()
		return normal
	}
	lo, hi, _ := s.SidebarBounds()
	return max(lo, min(hi, s.RightSidebarSize))
}
func (s *AppState) leftSidebarWidth() int {
	if s.Mobile() {
		return 0
	}
	if s.SidebarVisible {
		return s.ExpandedSidebarWidth()
	}
	if s.SidebarHidden {
		return 0
	}
	lo, _, _ := s.SidebarBounds()
	if s.SidebarSize >= SidebarCollapsedWidth && s.SidebarSize < lo {
		return s.SidebarSize
	}
	if s.SidebarCollapsedHidden {
		return 0
	}
	return SidebarCollapsedWidth
}
func (s *AppState) rightSidebarWidth() int {
	if s.Mobile() || s.RightSidebarMode == "hidden" {
		return 0
	}
	w := s.ExpandedRightSidebarWidth()
	if s.RightSidebarMode == "compact" {
		w = SidebarCollapsedWidth
		lo, _, _ := s.SidebarBounds()
		if s.RightSidebarSize >= SidebarCollapsedWidth && s.RightSidebarSize < lo {
			w = s.RightSidebarSize
		}
	}
	available := s.Cols - s.leftSidebarWidth() - MinimumPaneAreaWidth
	if w > available {
		w = SidebarCollapsedWidth
	}
	if w > available {
		w = 0
	}
	return w
}
func (s *AppState) RightSidebarArea() Rect {
	w := s.rightSidebarWidth()
	return Rect{X: max(0, s.Cols-w), W: w, H: max0(s.Rows)}
}

// RightSidebarCompact includes both the user's compact mode and narrow-screen adaptation.
func (s *AppState) RightSidebarCompact() bool {
	return s.RightSidebarMode == "compact" || s.RightSidebarArea().W == SidebarCollapsedWidth
}
