package model

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
)

type PopupPaneState struct {
	WorkspaceID   WorkspaceID
	PaneID        PaneID
	TerminalID    string
	Width, Height api.PopupSize
	ResizeLocked  bool
}

type CmdOpenPopup struct {
	WorkspaceID   WorkspaceID
	Cwd           string
	Argv          []string
	Width, Height api.PopupSize
}

func (CmdOpenPopup) isCommand() {}

type CmdClosePopup struct{}

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
	a.H += s.ActionBarArea().H
	size := func(value api.PopupSize, total int) int {
		if value.Percent > 0 {
			return max(0, min(total, max(3, total*value.Percent/100)))
		}
		return max(0, min(total, max(3, value.Cells)))
	}
	w, h := size(s.Popup.Width, a.W), size(s.Popup.Height, a.H)
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
	effects := s.closePopup()
	id := s.allocPane()
	s.Popup = &PopupPaneState{WorkspaceID: c.WorkspaceID, PaneID: id, TerminalID: fmt.Sprintf("p%d", id), Width: c.Width, Height: c.Height}
	return append(effects, EffSpawnPane{Pane: id, Cwd: c.Cwd, Argv: c.Argv}, EffSyncLayout{})
}

func (s *AppState) closePopup() []Effect {
	if s.Popup == nil {
		return nil
	}
	id := s.Popup.PaneID
	s.Popup = nil
	return []Effect{EffKillPane{Pane: id}}
}
