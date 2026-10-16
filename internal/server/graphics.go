package server

import (
	"encoding/base64"

	"github.com/kazuph/goterrace/internal/api"
)

func (s *Server) graphicsRequest(req *api.Request) api.Response {
	var params api.PaneGraphicsSetParams
	if err := req.DecodeParams(&params); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	pid, _, _, ok := s.findPane(params.PaneID)
	if !ok {
		return s.paneNotFound(req.ID, params.PaneID)
	}
	pane := s.sup.Get(pid)
	if pane == nil {
		return s.paneNotFound(req.ID, params.PaneID)
	}
	if !pane.GraphicsEnabled() {
		return api.Failure(req.ID, "feature_disabled", "kitty graphics is disabled")
	}
	switch req.Method {
	case api.MethodPaneGraphicsInfo:
		cw, ch := pane.CellPixels()
		return api.Success(req.ID, api.PaneGraphicsInfoResult{
			Type: "pane_graphics_info", CellWidthPX: cw, CellHeightPX: ch,
			PaneVisible: s.visiblePane(pid), MaxLayersPerPane: api.MaxGraphicsLayers, PixelMouse: true,
		})
	case api.MethodPaneGraphicsSet:
		data, err := base64.StdEncoding.DecodeString(params.DataBase64)
		if err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if failure := pane.SetGraphicsLayer(params, data, 0); failure != nil {
			return api.Failure(req.ID, failure.Code, failure.Message)
		}
	case api.MethodPaneGraphicsClear:
		pane.ClearGraphicsLayer(params.LayerID, 0)
	}
	return api.Success(req.ID, api.OkResult{Type: "ok"})
}
