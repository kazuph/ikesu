package model

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
)

type PopupPaneState struct {
	WorkspaceID   WorkspaceID
	PaneID        PaneID
	TerminalID    string
	Width, Height api.PopupSize
	ResizeLocked  bool
	Title         string
}

type CmdOpenPopup struct {
	WorkspaceID   WorkspaceID
	Cwd           string
	Argv          []string
	Env           map[string]string
	Width, Height api.PopupSize
	Title         string
}

func (CmdOpenPopup) isCommand() {}

type CmdClosePopup struct{ Pane PaneID }

func (CmdClosePopup) isCommand() {}

type CmdPopupResizeLock struct{ Locked bool }

func (CmdPopupResizeLock) isCommand() {}

func (s *AppState) PopupVisible() bool {
	w := s.ActiveWorkspace()
	return s.Popup != nil && w != nil && s.Popup.WorkspaceID == w.ID
}

func (s *AppState) PopupRect() layout.Rect {
	if s.Popup == nil {
		return layout.Rect{}
	}
	a := s.PanesArea()
	size := func(value api.PopupSize, total, minimum int) int {
		if value.Percent > 0 {
			return max(minimum, min(total, total*value.Percent/100))
		}
		return max(minimum, min(total, value.Cells))
	}
	w, h := size(s.Popup.Width, a.W, 6), size(s.Popup.Height, a.H, 4)
	return layout.Rect{X: a.X + (a.W-w)/2, Y: a.Y + (a.H-h)/2, W: w, H: h}
}

func (s *AppState) openPopup(c CmdOpenPopup) []Effect {
	owner := false
	for _, w := range s.Workspaces {
		if w.ID == c.WorkspaceID {
			owner = true
			break
		}
	}
	if !owner {
		return nil
	}
	if s.Popups == nil {
		s.Popups = map[WorkspaceID]*PopupPaneState{}
		if s.Popup != nil {
			s.Popups[s.Popup.WorkspaceID] = s.Popup
		}
	}
	if s.Popups[c.WorkspaceID] != nil {
		return nil
	}
	var effects []Effect
	s.nextRuntimePaneID = max(s.nextRuntimePaneID, s.nextPaneID) + 1
	id := s.nextRuntimePaneID
	s.Popup = &PopupPaneState{WorkspaceID: c.WorkspaceID, PaneID: id, TerminalID: fmt.Sprintf("p%d", id), Width: c.Width, Height: c.Height, Title: c.Title}
	s.Popups[c.WorkspaceID] = s.Popup
	return append(effects, EffSpawnPane{Pane: id, Cwd: c.Cwd, Argv: c.Argv, Env: c.Env}, EffSyncLayout{})
}

func (s *AppState) PopupByPane(id PaneID) *PopupPaneState {
	if s.Popup != nil && s.Popup.PaneID == id {
		return s.Popup
	}
	for _, pop := range s.Popups {
		if pop.PaneID == id {
			return pop
		}
	}
	return nil
}
func (s *AppState) closePopup() []Effect {
	if !s.PopupVisible() {
		return nil
	}
	return s.closePopupPane(s.Popup)
}
func (s *AppState) closePopupPane(pop *PopupPaneState) []Effect {
	if pop == nil {
		return nil
	}
	delete(s.Popups, pop.WorkspaceID)
	if s.Popup == pop {
		s.Popup = nil
	}
	return []Effect{EffKillPane{Pane: pop.PaneID}}
}
func (s *AppState) syncPopupOwner() []Effect {
	if s.Popups == nil {
		return nil
	}
	owners := map[WorkspaceID]bool{}
	for _, ws := range s.Workspaces {
		owners[ws.ID] = true
	}
	var effects []Effect
	for id, pop := range s.Popups {
		if !owners[id] {
			effects = append(effects, s.closePopupPane(pop)...)
		}
	}
	if ws := s.ActiveWorkspace(); ws != nil {
		if pop := s.Popups[ws.ID]; pop != nil {
			s.Popup = pop
		}
	}
	return effects
}
