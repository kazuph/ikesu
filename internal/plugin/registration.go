package plugin

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// PreparedLink is the exact value Link persists, before any registration is sent.
func (s *Store) PreparedLink(path string, enabled bool, source *Source) (Installed, error) {
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
	return p, nil
}

// Registration returns the persisted record without refreshing it from disk.
func (s *Store) Registration(id string) (*Installed, error) {
	var found *Installed
	err := s.transaction(false, func(entries *[]Installed) error {
		for _, p := range *entries {
			if p.ID == id {
				copy := p
				found = &copy
				break
			}
		}
		return nil
	})
	return found, err
}

// RestoreRegistration only undoes this install's write. Concurrent link,
// disable and unlink operations must survive even when a response was lost.
func (s *Store) RestoreRegistration(expected Installed, previous *Installed) error {
	return s.transaction(true, func(entries *[]Installed) error {
		for i, p := range *entries {
			if p.ID != expected.ID {
				continue
			}
			if previous != nil && reflect.DeepEqual(p, *previous) {
				return nil
			}
			if !reflect.DeepEqual(p, expected) {
				return fmt.Errorf("plugin registration conflict: preserving concurrent change to %s", expected.ID)
			}
			if previous == nil {
				*entries = append((*entries)[:i], (*entries)[i+1:]...)
			} else {
				(*entries)[i] = *previous
			}
			return nil
		}
		return fmt.Errorf("plugin registration conflict: preserving concurrent unlink of %s", expected.ID)
	})
}
