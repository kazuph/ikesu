package client

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
)

type localSettings struct {
	clientVersion string
	session       string
	config        *config.Config
	warning       string
	preferences   *endpoint.ViewPreferences
	path          string
}

func (s *localSettings) apply(edit config.Edit) (config.Config, error) {
	var current config.Config
	if s.config != nil {
		current = *s.config
	}
	return edit.ApplyTo(current)
}

func loadLocalSettings(session string) (*localSettings, error) {
	if !api.ValidSessionName(session) {
		return nil, fmt.Errorf("invalid client session name")
	}
	s := &localSettings{session: session, path: endpoint.ViewPreferencesPath(session)}
	c, err := config.Load()
	if err != nil {
		s.warning = "client configuration: " + err.Error()
	} else {
		s.config = &c
	}
	preferences, err := endpoint.LoadViewPreferences(s.path)
	if err != nil {
		return nil, err
	}
	s.preferences = preferences
	return s, nil
}

func (s *localSettings) save(preferences endpoint.ViewPreferences) error {
	if err := endpoint.SaveViewPreferences(s.path, preferences); err != nil {
		return err
	}
	s.preferences = &preferences
	return nil
}
