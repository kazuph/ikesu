package server

import (
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
)

func (s *Server) notificationShow(req *api.Request) api.Response {
	var p api.NotificationShowParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure("", api.ErrInvalidRequest, err.Error())
	}
	if p.Title == nil {
		return api.Failure("", api.ErrInvalidRequest, "notification title must be a string")
	}
	position := ""
	if p.Position != nil {
		position = *p.Position
		switch position {
		case "top-left", "top-right", "bottom-left", "bottom-right":
		default:
			return api.Failure("", api.ErrInvalidRequest, "invalid notification position")
		}
	}
	title := strings.TrimSpace(*p.Title)
	if title == "" {
		return api.Failure(req.ID, "invalid_params", "notification title is empty")
	}
	body := ""
	if p.Body != nil {
		body = strings.TrimSpace(*p.Body)
	}
	result := api.NotificationShowResult{Type: "notification_show", Reason: "disabled"}
	if s.cfg.ToastDelivery() == config.ToastOff {
		return api.Success(req.ID, result)
	}
	s.mu.Lock()
	foreground := s.client
	s.mu.Unlock()
	if (s.cfg.ToastDelivery() == config.ToastTerminal || s.cfg.ToastDelivery() == config.ToastSystem) && foreground == nil {
		result.Reason = "no_foreground_client"
		return api.Success(req.ID, result)
	}
	if s.cfg.ToastDelivery() == config.ToastGTX && !s.app.ShowNotification(title, body, position) {
		result.Reason = "busy"
		return api.Success(req.ID, result)
	}
	result.Shown, result.Reason = true, "shown"
	kind := "info"
	switch p.Sound {
	case "done":
		kind = "finished"
	case "request":
		kind = "needs_attention"
	}
	if kind != "info" || s.cfg.ToastDelivery() == config.ToastSystem {
		c := foreground
		if c != nil && c.available < endpoint.NotificationGeneration {
			return api.Failure(req.ID, "endpoint_action_unsupported", "notification delivery requires endpoint generation 4")
		}
		if c != nil {
			n := endpoint.Notification{Title: title, Body: body, Kind: kind, SystemToast: s.cfg.ToastDelivery() == config.ToastSystem}
			if err := c.send(clientMsg{Type: "notify", Notification: &n}); err != nil {
				s.dropConn(c)
			}
		}
	}
	return api.Success(req.ID, result)
}
