package server

import (
	"fmt"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
)

func (s *Server) reloadConfig() api.ConfigReloadResult {
	result := api.ConfigReloadResult{Type: "config_reload", Status: "applied", Diagnostics: []string{}, ConfigPath: s.configPath}
	c, err := config.LoadReloaded(s.cfg)
	if err != nil {
		result.Status = "failed"
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("config parse error: %v; keeping current config", err))
		s.app.SetConfigWarning(result.Diagnostics[0])
		for _, display := range s.displayClients() {
			if display.viewer != nil {
				display.viewer.ConfigError(s.app, result.Diagnostics[0])
			}
		}
		return result
	}
	if len(c.Warnings) > 0 {
		result.Status = "partial"
		result.Diagnostics = append(result.Diagnostics, c.Warnings...)
	}
	s.app.ApplyReloadedConfig(c)
	for _, display := range s.displayClients() {
		if display.viewer == nil || display.configEditor == nil {
			continue
		}
		display.configEditor(config.Edit{Kind: "reload"}, func(cfg config.Config, err error) {
			if err != nil {
				display.viewer.ConfigError(s.app, fmt.Sprintf("config parse error: %v; keeping current config", err))
				return
			}
			display.viewer.ApplyReloadedConfig(cfg)
		})
	}
	return result
}
