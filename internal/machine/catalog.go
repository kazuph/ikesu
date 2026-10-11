// Package machine owns saved SSH profiles, independently of endpoint clients.
package machine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Profile struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

type Catalog struct{ Path string }

// Keep an empty catalog watchable without monitoring unrelated session writes.
// The existing catalog lock also protects a concurrent first machine add.
func (c Catalog) Ensure() error {
	if _, err := os.Stat(c.Path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return c.update(func(_ []Profile) ([]Profile, error) {
		data, err := os.ReadFile(c.Path)
		if os.IsNotExist(err) {
			return []Profile{}, nil
		}
		if err != nil {
			return nil, err
		}
		var profiles []Profile
		if err := json.Unmarshal(data, &profiles); err != nil {
			return nil, err
		}
		return profiles, nil
	})
}

func (c Catalog) Load() []Profile {
	b, err := os.ReadFile(c.Path)
	p := []Profile{}
	if err != nil || json.Unmarshal(b, &p) != nil || p == nil {
		return []Profile{}
	}
	return p
}

func Resolve(profiles []Profile, ref string, enabled bool) (Profile, error) {
	var result Profile
	n := 0
	for _, p := range profiles {
		if p.ID == ref || p.Label == ref {
			result = p
			n++
		}
	}
	if n == 0 {
		return Profile{}, fmt.Errorf("machine %q was not found", ref)
	}
	if n > 1 {
		return Profile{}, fmt.Errorf("machine %q is ambiguous", ref)
	}
	if enabled && !result.Enabled {
		return Profile{}, fmt.Errorf("machine %q is disabled", ref)
	}
	return result, nil
}

func ValidateAdd(profiles []Profile, target, label, session string) error {
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("--label is required")
	}
	if strings.TrimSpace(target) == "" || strings.HasPrefix(target, "-") {
		return fmt.Errorf("SSH target is required and must not start with '-'")
	}
	if session == "" || session == "." || session == ".." || strings.ContainsAny(session, `/\\`) {
		return fmt.Errorf("invalid remote session %q", session)
	}
	for _, p := range profiles {
		if p.Label == label {
			return fmt.Errorf("machine label %q already exists", label)
		}
		if p.Target == target && p.Session == session {
			return fmt.Errorf("machine target/session already exists")
		}
	}
	return nil
}

func ProbeArgs(target string) []string {
	return []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", target, "true"}
}

func Probe(ctx context.Context, target string) error {
	if target == "" || strings.HasPrefix(target, "-") {
		return fmt.Errorf("invalid SSH target")
	}
	b, err := exec.CommandContext(ctx, "ssh", ProbeArgs(target)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh probe to %q failed: %v: %s; machine was not saved", target, err, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c Catalog) Add(ctx context.Context, target, label, session string) (Profile, error) {
	if err := ValidateAdd(c.Load(), target, label, session); err != nil {
		return Profile{}, err
	}
	if err := Probe(ctx, target); err != nil {
		return Profile{}, err
	}
	return c.AddVerified(target, label, session)
}

// AddVerified is used only after the shared reachability probe has succeeded.
func (c Catalog) AddVerified(target, label, session string) (Profile, error) {
	var p Profile
	err := c.update(func(profiles []Profile) ([]Profile, error) {
		if err := ValidateAdd(profiles, target, label, session); err != nil {
			return nil, err
		}
		var id [6]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		p = Profile{ID: "m" + hex.EncodeToString(id[:]), Label: label, Target: target, Session: session, Enabled: true}
		return append(profiles, p), nil
	})
	return p, err
}

func (c Catalog) Change(ref, action, label string, expected *Profile) (Profile, error) {
	var result Profile
	err := c.update(func(profiles []Profile) ([]Profile, error) {
		p, err := Resolve(profiles, ref, false)
		if err != nil {
			return nil, err
		}
		if expected != nil && p != *expected {
			return nil, fmt.Errorf("saved machine changed; reopen the machine list")
		}
		result = p
		if action == "rename" {
			if strings.TrimSpace(label) == "" {
				return nil, fmt.Errorf("--label is required")
			}
			for _, other := range profiles {
				if other.ID != p.ID && other.Label == label {
					return nil, fmt.Errorf("machine label %q already exists", label)
				}
			}
		}
		for i := range profiles {
			if profiles[i].ID == p.ID {
				switch action {
				case "rename":
					profiles[i].Label = label
				case "enable":
					profiles[i].Enabled = true
				case "disable":
					profiles[i].Enabled = false
				case "remove":
					return append(profiles[:i], profiles[i+1:]...), nil
				default:
					return nil, fmt.Errorf("unsupported machine action %q", action)
				}
				result = profiles[i]
				break
			}
		}
		return profiles, nil
	})
	return result, err
}

func (c Catalog) update(fn func([]Profile) ([]Profile, error)) error {
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(c.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	profiles, err := fn(c.Load())
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".machines-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), c.Path)
}
