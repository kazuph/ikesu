package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
)

type localSettings struct {
	config      *config.Config
	warning     string
	preferences *endpoint.ViewPreferences
	path        string
}

func loadLocalSettings(session string) (*localSettings, error) {
	if !api.ValidSessionName(session) {
		return nil, fmt.Errorf("invalid client session name")
	}
	s := &localSettings{path: filepath.Join(api.StateDir(), "client-shell", "local-"+session+".json")}
	c, err := config.Load()
	if err != nil {
		s.warning = "client configuration: " + err.Error()
	} else {
		s.config = &c
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var preferences endpoint.ViewPreferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return nil, fmt.Errorf("client preferences: %w", err)
	}
	if preferences.SidebarWidth < 1 || preferences.SidebarSectionSplit != nil && (*preferences.SidebarSectionSplit < .1 || *preferences.SidebarSectionSplit > .9) {
		return nil, fmt.Errorf("invalid client sidebar preferences")
	}
	s.preferences = &preferences
	return s, nil
}

func (s *localSettings) save(preferences endpoint.ViewPreferences) error {
	data, err := json.Marshal(preferences)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".client-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.preferences = &preferences
	return nil
}
