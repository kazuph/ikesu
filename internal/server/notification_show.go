package server

import (
	"strings"
	"time"
	"unicode"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
)

// Explicit API notification limits from FIX-1; background cooldown is separate.
const apiNotificationInterval = time.Second

func notificationText(text string, limit int) string {
	out := make([]rune, 0, limit)
	space := false
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		} else if unicode.IsControl(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if space {
				continue
			}
			r, space = ' ', true
		} else {
			space = false
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return strings.TrimSpace(string(out))
}

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
	title := notificationText(*p.Title, 80)
	if title == "" {
		return api.Failure(req.ID, "invalid_params", "notification title is empty")
	}
	body := ""
	if p.Body != nil {
		body = notificationText(*p.Body, 240)
	}
	result := api.NotificationShowResult{Type: "notification_show", Reason: "disabled"}
	if s.cfg.ToastDelivery() == config.ToastOff {
		return api.Success(req.ID, result)
	}
	if s.cfg.ToastDelivery() == config.ToastIKESU && s.app.NotificationBusy() {
		result.Reason = "busy"
		return api.Success(req.ID, result)
	}
	if time.Since(s.lastAPINotification) < apiNotificationInterval {
		result.Reason = "rate_limited"
		return api.Success(req.ID, result)
	}
	s.mu.Lock()
	foreground := s.client
	s.mu.Unlock()
	if (s.cfg.ToastDelivery() == config.ToastTerminal || s.cfg.ToastDelivery() == config.ToastSystem) && foreground == nil {
		result.Reason = "no_foreground_client"
		return api.Success(req.ID, result)
	}
	if s.cfg.ToastDelivery() == config.ToastTerminal {
		sequence := foreground.terminal.NotificationBytes(title, body)
		if sequence != "" {
			if err := foreground.send(clientMsg{Type: "output", Data: encode64([]byte(sequence))}); err != nil {
				s.dropConn(foreground)
				result.Reason = "no_foreground_client"
				return api.Success(req.ID, result)
			}
		}
	}
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
				result.Reason = "no_foreground_client"
				return api.Success(req.ID, result)
			}
		}
	}
	if s.cfg.ToastDelivery() == config.ToastIKESU {
		s.app.ShowNotification(title, body, position)
	}
	result.Shown, result.Reason = true, "shown"
	s.lastAPINotification = time.Now()
	return api.Success(req.ID, result)
}
