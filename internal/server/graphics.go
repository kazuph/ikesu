package server

import (
	"encoding/base64"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
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
		if cw <= 0 || ch <= 0 {
			return api.Failure(req.ID, "cell_size_unavailable", "exact terminal cell pixels are unavailable")
		}
		info := api.PaneGraphicsInfoResult{
			Type: "pane_graphics_info", CellWidthPX: cw, CellHeightPX: ch,
			PaneVisible: s.graphicsPaneVisible(pid), FileFrameDamage: true, MaxLayersPerPane: api.MaxGraphicsLayers, PixelMouse: true,
		}
		if s.directGraphicsViewer(pid, false) != nil {
			if source := s.graphicsSource(); source != nil {
				info.FileFrameDirectory, info.FileFrameFormats = source.source, []string{"rgba", "bgra"}
				info.FileFrameMaxBytes, info.FileFrameDirectMaxBytes = api.MaxGraphicsFileInlineBytes, api.MaxGraphicsFileDirectBytes
				info.FileFrameTransport = endpoint.DirectKittyTransport
			}
		}
		return api.Success(req.ID, info)
	case api.MethodPaneGraphicsSet:
		data, err := base64.StdEncoding.DecodeString(params.DataBase64)
		if err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if len(data) > api.MaxGraphicsSetBytes {
			return api.Failure(req.ID, "image_too_large", "image data is too large")
		}
		if failure := s.sup.SetGraphicsLayer(pid, params, data, 0); failure != nil {
			return api.Failure(req.ID, failure.Code, failure.Message)
		}
	case api.MethodPaneGraphicsClear:
		if failure := pane.ClearGraphicsLayer(params.LayerID, 0); failure != nil {
			return api.Failure(req.ID, failure.Code, failure.Message)
		}
	}
	return api.Success(req.ID, api.OkResult{Type: "ok"})
}
