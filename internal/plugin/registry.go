package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
)

type Source struct {
	Kind            string  `json:"kind"`
	Owner           string  `json:"owner,omitempty"`
	Repo            string  `json:"repo,omitempty"`
	Subdir          string  `json:"subdir,omitempty"`
	RequestedRef    string  `json:"requested_ref,omitempty"`
	ResolvedCommit  string  `json:"resolved_commit,omitempty"`
	ManagedPath     string  `json:"managed_path,omitempty"`
	InstalledUnixMS *uint64 `json:"installed_unix_ms,omitempty"`
}
type Installed struct {
	Manifest
	ManifestPath string   `json:"manifest_path"`
	Root         string   `json:"plugin_root"`
	Enabled      bool     `json:"enabled"`
	Source       Source   `json:"source"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Store uses a session-independent registry and an OS lock so offline CLI
// changes and simultaneous servers cannot lose each other's updates.
type Store struct{ ConfigRoot, StateRoot, Version string }

func DefaultStore(version string) *Store {
	return &Store{ConfigRoot: filepath.Dir(config.Path()), StateRoot: api.StateDir(), Version: version}
}
func (s *Store) ConfigDir(id string) (string, error) {
	if !packageID.MatchString(id) || id == "." || id == ".." {
		return "", fmt.Errorf("invalid plugin id %q", id)
	}
	p := filepath.Join(s.ConfigRoot, "plugins", "config", id)
	return p, os.MkdirAll(p, 0700)
}
func (s *Store) StateDir(id string) (string, error) {
	if !packageID.MatchString(id) || id == "." || id == ".." {
		return "", fmt.Errorf("invalid plugin id %q", id)
	}
	p := filepath.Join(s.StateRoot, "plugins", id)
	return p, os.MkdirAll(p, 0700)
}
func (s *Store) transaction(update bool, fn func(*[]Installed) error) error {
	if err := os.MkdirAll(s.ConfigRoot, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.ConfigRoot, ".plugins.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	path := filepath.Join(s.ConfigRoot, "plugins.json")
	entries := []Installed{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("plugin registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = fn(&entries); err != nil {
		return err
	}
	if !update {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	data, err = json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.ConfigRoot, ".plugins-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
func (s *Store) Link(path string, enabled bool, source *Source) (Installed, error) {
	m, path, err := ReadManifest(path, s.Version)
	if err != nil {
		return Installed{}, err
	}
	p := Installed{Manifest: m, ManifestPath: path, Root: filepath.Dir(path), Enabled: enabled, Source: Source{Kind: "local"}}
	if source != nil {
		p.Source = *source
	}
	if m.Platforms == nil {
		p.Warnings = append(p.Warnings, "manifest does not declare platforms; platform support unknown")
	}
	if _, err = s.ConfigDir(m.ID); err != nil {
		return p, err
	}
	if _, err = s.StateDir(m.ID); err != nil {
		return p, err
	}
	err = s.transaction(true, func(entries *[]Installed) error {
		for i, e := range *entries {
			if e.ID == m.ID {
				(*entries)[i] = p
				return nil
			}
		}
		*entries = append(*entries, p)
		return nil
	})
	return p, err
}
func (s *Store) List(id string) ([]Installed, error) {
	out := []Installed{}
	err := s.transaction(false, func(entries *[]Installed) error {
		for _, p := range *entries {
			if id != "" && id != p.ID {
				continue
			}
			m, _, err := ReadManifest(p.ManifestPath, s.Version)
			if err != nil {
				p.Warnings = append(p.Warnings, err.Error())
			} else {
				p.Manifest = m
			}
			out = append(out, p)
		}
		return nil
	})
	return out, err
}
func (s *Store) SetEnabled(id string, enabled bool) (Installed, error) {
	var out Installed
	err := s.transaction(true, func(entries *[]Installed) error {
		for i := range *entries {
			if (*entries)[i].ID == id {
				(*entries)[i].Enabled = enabled
				out = (*entries)[i]
				return nil
			}
		}
		return fmt.Errorf("plugin %q not found", id)
	})
	return out, err
}
func (s *Store) Unlink(id string) (bool, error) {
	removed := false
	err := s.transaction(true, func(entries *[]Installed) error {
		for i, p := range *entries {
			if p.ID == id {
				*entries = append((*entries)[:i], (*entries)[i+1:]...)
				removed = true
				break
			}
		}
		return nil
	})
	return removed, err
}
