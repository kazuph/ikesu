package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"os"
)

func (s *Server) popupRequest(req *api.Request) api.Response {
	notOpen := func() api.Response { return api.Failure(req.ID, "popup_not_open", "no popup is open") }
	switch req.Method {
	case "pane.command.execute":
		var p api.PaneCommandExecuteParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if p.Action != "popup" && p.Action != "overlay" {
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
		if p.Action == "overlay" {
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/zsh"
			}
			overlay, err := s.app.OpenOverlay(pid, pane.Snapshot().Cwd, []string{shell, "-c", p.Command}, nil)
			if err != nil {
				code := api.ErrInvalidRequest
				if err.Error() == "ui_busy" {
					code = "ui_busy"
				}
				return api.Failure(req.ID, code, err.Error())
			}
			info, _ := s.paneInfo(overlay)
			return api.Success(req.ID, api.CommandExecutedResult{Type: "command_executed", Pane: &info})
		}
		tid, err := s.app.OpenPopup(pane.Snapshot().Cwd, p.Command, p.Width, p.Height)
		if err != nil {
			code := api.ErrInvalidRequest
			if err.Error() == "popup already open" {
				code = "command_failed"
			}
			return api.Failure(req.ID, code, err.Error())
		}
		return api.Success(req.ID, api.CommandExecutedResult{Type: "command_executed", PopupTerminalID: &tid})
	case "popup.close":
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if !s.app.State().PopupVisible() {
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
		lines := s.app.Supervisor().Get(s.app.PopupPaneID(p.TerminalID)).BufferLines()
		if uint64(p.Anchor.Row) >= uint64(len(lines)) || uint64(p.Cursor.Row) >= uint64(len(lines)) {
			return api.Failure(req.ID, api.ErrInvalidRequest, "terminal row is unavailable")
		}
		return api.Success(req.ID, api.PopupSelectionResult{Type: "popup_selection", TerminalID: p.TerminalID, ContentRevision: info.ContentRevision, Text: s.app.PopupSelectionText(p.TerminalID, p.Anchor, p.Cursor)})
	}
	return api.Failure(req.ID, api.ErrUnknownMethod, "unknown popup operation")
}

func (s *Server) rejectOverlayLayoutRequest(req *api.Request) (api.Response, bool) {
	switch req.Method {
	case "pane.rename", "pane.zoom", "pane.resize", "pane.move", "pane.swap", "pane.split", "pane.adjust_layout":
	default:
		return api.Response{}, false
	}
	var params map[string]any
	if err := req.DecodeParams(&params); err != nil {
		return api.Response{}, false
	}
	blocked := false
	s.app.Do(func() {
		if overlay := s.app.State().Overlay; overlay != nil {
			for _, key := range []string{"pane_id", "source_pane_id", "target_pane_id"} {
				if params[key] == fmtPaneID(overlay.PaneID) {
					blocked = true
				}
			}
		}
	})
	if blocked {
		return api.Failure(req.ID, "unsupported", "overlay panes do not participate in tiled layout operations"), true
	}
	return api.Response{}, false
}
