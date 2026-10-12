package server

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
)

func (s *Server) reloadConfig() api.ConfigReloadResult {
	result := api.ConfigReloadResult{Type: "config_reload", Status: "applied", Diagnostics: []string{}}
	c, err := config.LoadReloaded(s.cfg)
	if err != nil {
		result.Status = "failed"
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("config parse error: %v; keeping current config", err))
		s.app.SetConfigWarning(result.Diagnostics[0])
		return result
	}
	if len(c.Warnings) > 0 {
		result.Status = "partial"
		result.Diagnostics = append(result.Diagnostics, c.Warnings...)
	}
	s.app.ApplyReloadedConfig(c)
	return result
}
