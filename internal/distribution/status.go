package distribution

import (
	"os"
	"path/filepath"
	"strings"
)

type Status struct {
	Product         string   `json:"product"`
	Installed       []string `json:"installed"`
	Current         string   `json:"current_version"`
	Registered      string   `json:"registered_manifest"`
	DirectWrapper   string   `json:"direct_wrapper"`
	WrapperMismatch bool     `json:"wrapper_mismatch"`
	Latest          string   `json:"latest"`
	LatestError     string   `json:"latest_error,omitempty"`
}

func (i *Installer) Status() (Status, error) {
	s := Status{Product: i.Product.Name, Installed: []string{}}
	entries, err := os.ReadDir(i.Directory())
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	for _, e := range entries {
		if _, err := version(e.Name()); err == nil && e.IsDir() {
			s.Installed = append(s.Installed, e.Name())
		}
	}
	data, err := os.ReadFile(filepath.Join(i.Directory(), "current-version"))
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	s.Current = strings.TrimSpace(string(data))
	plugins, err := i.Store.List("")
	if err != nil {
		return s, err
	}
	for _, p := range plugins {
		if strings.HasPrefix(p.ManifestPath, i.Directory()+string(filepath.Separator)) {
			s.Registered = p.ManifestPath
			break
		}
	}
	if i.Product.Name == "terminal-browser" {
		wrapper, err := snapshotFile(filepath.Join(i.Home, ".local", "bin", "tb"))
		if err != nil {
			return s, err
		}
		if wrapper.exists {
			if wrapper.link != "" {
				s.DirectWrapper = wrapper.link
			} else {
				s.DirectWrapper = string(wrapper.data)
			}
			wanted := filepath.Join(filepath.Dir(filepath.Dir(s.Registered)), "bin", "terminal-browser")
			s.WrapperMismatch = s.Registered != "" && !strings.Contains(s.DirectWrapper, wanted)
		}
	}
	return s, nil
}
