package model

import "github.com/kazuph/goterrace/internal/layout"

// OverlayPaneState belongs to a tab, but is never a leaf of its layout.
type OverlayPaneState struct {
	WorkspaceID          WorkspaceID
	TabID                int
	PaneID, TargetPaneID PaneID
}
type CmdOpenOverlay struct {
	TargetPaneID PaneID
	Cwd          string
	Argv         []string
	Env          map[string]string
}

func (CmdOpenOverlay) isCommand() {}

type CmdCloseOverlay struct{}

func (CmdCloseOverlay) isCommand() {}

func (s *AppState) OverlayVisible() bool {
	o := s.Overlay
	ws := s.ActiveWorkspace()
	return o != nil && ws != nil && ws.ID == o.WorkspaceID && ws.TabRef() != nil && ws.TabRef().ID == o.TabID && ws.ActiveLayout().HasPane(o.TargetPaneID)
}
func (s *AppState) OverlayRect() (layout.Rect, bool) {
	o := s.Overlay
	if o == nil {
		return layout.Rect{}, false
	}
	for _, ws := range s.Workspaces {
		if ws.ID != o.WorkspaceID {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab.ID != o.TabID {
				continue
			}
			for _, pr := range tab.PaneRects(s.PanesArea()) {
				if pr.ID == o.TargetPaneID {
					return pr.Rect, true
				}
			}
		}
	}
	return layout.Rect{}, false
}

// DisplayPaneRects projects the overlay onto precisely one existing rectangle.
func (s *AppState) DisplayPaneRects() []layout.PaneRect {
	ws := s.ActiveWorkspace()
	if ws == nil || ws.TabRef() == nil {
		return nil
	}
	out := ws.TabRef().PaneRects(s.PanesArea())
	if s.OverlayVisible() {
		for i := range out {
			out[i].IsFocused = out[i].ID == s.Overlay.TargetPaneID
			if out[i].ID == s.Overlay.TargetPaneID {
				out[i].ID = s.Overlay.PaneID
			}
		}
	}
	return out
}
func (s *AppState) openOverlay(c CmdOpenOverlay) []Effect {
	wi, ti := s.FindPane(c.TargetPaneID)
	if wi != s.Active || wi < 0 || s.Workspaces[wi].ActiveTab != ti || !s.Workspaces[wi].Tabs[ti].Layout.HasPane(c.TargetPaneID) {
		return nil
	}
	effects := s.closeOverlay()
	s.nextRuntimePaneID = max(s.nextRuntimePaneID, s.nextPaneID) + 1
	id := s.nextRuntimePaneID
	ws := s.Workspaces[wi]
	s.Overlay = &OverlayPaneState{WorkspaceID: ws.ID, TabID: ws.Tabs[ti].ID, PaneID: id, TargetPaneID: c.TargetPaneID}
	return append(effects, EffSpawnPane{Pane: id, Cwd: c.Cwd, Argv: c.Argv, Env: c.Env}, EffSyncLayout{})
}
func (s *AppState) closeOverlay() []Effect {
	if s.Overlay == nil {
		return nil
	}
	id := s.Overlay.PaneID
	s.Overlay = nil
	return []Effect{EffKillPane{Pane: id}, EffSyncLayout{}}
}
