package endpoint

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"os"
	"path/filepath"
)

func ViewPreferencesPath(session string) string {
	return filepath.Join(api.StateDir(), "client-shell", "local-"+session+".json")
}

func LoadViewPreferences(path string) (*ViewPreferences, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p ViewPreferences
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("client preferences: %w", err)
	}
	if err = p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p ViewPreferences) Validate() error {
	if p.SidebarWidth < 1 || p.RightSidebarWidth < 0 || p.SidebarSectionSplit != nil && (*p.SidebarSectionSplit < .1 || *p.SidebarSectionSplit > .9) {
		return fmt.Errorf("invalid client sidebar preferences")
	}
	switch p.RightSidebarMode {
	case "", "compact", "hidden":
	default:
		return fmt.Errorf("invalid right sidebar preference: %q", p.RightSidebarMode)
	}
	return nil
}

func SaveViewPreferences(path string, p ViewPreferences) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".client-*.json")
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
	return os.Rename(f.Name(), path)
}
