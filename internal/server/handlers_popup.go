package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (s *Server) popupRequest(req *api.Request) api.Response {
	notOpen := func() api.Response { return api.Failure(req.ID, "popup_not_open", "no popup is open") }
	switch req.Method {
	case "pane.command.execute":
		var p api.PaneCommandExecuteParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if p.Action != "popup" {
			return api.Failure(req.ID, api.ErrInvalidRequest, "unsupported command action: "+p.Action)
		}
		pid, _, _, ok := s.findPane(p.PaneID)
		if !ok {
			return s.paneNotFound(req.ID, p.PaneID)
		}
		pane := s.app.Supervisor().Get(pid)
		if pane == nil {
			return s.paneNotFound(req.ID, p.PaneID)
		}
		tid, err := s.app.OpenPopup(pane.Snapshot().Cwd, p.Command, p.Width, p.Height)
		if err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return api.Success(req.ID, api.CommandExecutedResult{Type: "command_executed", PopupTerminalID: &tid})
	case "popup.close":
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if s.app.State().Popup == nil {
			return notOpen()
		}
		s.app.ExecCmd(model.CmdClosePopup{})
		return api.Success(req.ID, api.OkResult{Type: "ok"})
	case "popup.get":
		var p api.PopupTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		info := s.app.PopupTerminal(p.TerminalID)
		if info == nil {
			return notOpen()
		}
		return api.Success(req.ID, info)
	case "popup.scroll":
		var p api.PopupScrollParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		info := s.app.ScrollPopup(p.TerminalID, p.OffsetFromBottom)
		if info == nil {
			return notOpen()
		}
		return api.Success(req.ID, info)
	case "popup.selection.read":
		var p api.PopupSelectionReadParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		info := s.app.PopupTerminal(p.TerminalID)
		if info == nil {
			return notOpen()
		}
		if info.ContentRevision != p.ContentRevision {
			return api.Failure(req.ID, api.ErrStaleContent, "popup content changed")
		}
		lines := s.app.Supervisor().Get(s.app.State().Popup.PaneID).BufferLines()
		if uint64(p.Anchor.Row) >= uint64(len(lines)) || uint64(p.Cursor.Row) >= uint64(len(lines)) {
			return api.Failure(req.ID, api.ErrInvalidRequest, "terminal row is unavailable")
		}
		return api.Success(req.ID, api.PopupSelectionResult{Type: "popup_selection", TerminalID: p.TerminalID, ContentRevision: info.ContentRevision, Text: s.app.PopupSelectionText(p.Anchor, p.Cursor)})
	}
	return api.Failure(req.ID, api.ErrUnknownMethod, "unknown popup operation")
}
