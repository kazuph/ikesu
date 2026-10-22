package distribution

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Status struct {
	Product           string   `json:"product"`
	Installed         []string `json:"installed"`
	Current           string   `json:"current_version"`
	Registered        string   `json:"registered_manifest"`
	DirectWrapper     string   `json:"direct_wrapper"`
	WrapperMismatch   bool     `json:"wrapper_mismatch"`
	Latest            string   `json:"latest"`
	LatestError       string   `json:"latest_error,omitempty"`
	RegisteredVersion string   `json:"registered_version,omitempty"`
	RunningVersion    string   `json:"running_version,omitempty"`
	RunningUnknown    bool     `json:"running_unknown,omitempty"`
	SwitchPending     bool     `json:"switch_pending,omitempty"`
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
		if strings.HasPrefix(p.ManifestPath, i.Directory()+string(filepath.Separator)) || i.Product.PluginID != "" && p.ID == i.Product.PluginID {
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
	if i.Product.Name == "mac-dialog-decisions" {
		if err := i.macStatus(&s); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (i *Installer) macStatus(s *Status) error {
	registeredRoot := ""
	if s.Registered != "" {
		registeredRoot = filepath.Dir(s.Registered)
		data, err := os.ReadFile(filepath.Join(registeredRoot, "VERSION"))
		if err == nil {
			s.RegisteredVersion = strings.TrimSpace(string(data))
		}
	}
	directory, err := i.Store.StateDir(i.Product.PluginID)
	if err != nil {
		return err
	}
	var owner struct {
		PID                                          int `json:"pid"`
		Owner, Node, Script, Root, Version, StateDir string
	}
	data, err := os.ReadFile(filepath.Join(directory, "daemon.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &owner); err != nil {
		s.RunningUnknown = true
		s.SwitchPending = true
		return nil
	}
	if owner.PID <= 0 {
		return nil
	}
	if err := syscall.Kill(owner.PID, 0); err != nil && err != syscall.EPERM {
		return nil
	}
	s.RunningUnknown = true
	s.SwitchPending = registeredRoot != "" && registeredRoot != owner.Root
	if owner.Node == "" || owner.Script == "" {
		return nil
	}
	command, err := exec.Command("ps", "-o", "command=", "-p", fmt.Sprint(owner.PID)).Output()
	if err != nil || strings.TrimSpace(string(command)) != owner.Node+" "+owner.Script+" run" {
		return nil
	}
	actual, err := filepath.EvalSymlinks(directory)
	if err != nil || owner.StateDir != actual {
		return nil
	}
	var state struct {
		PID                  int `json:"pid"`
		Owner, Root, Version string
		AllMachines          bool `json:"all_machines"`
	}
	data, err = os.ReadFile(filepath.Join(directory, "daemon-state.json"))
	if err != nil {
		return nil
	}
	if json.Unmarshal(data, &state) != nil || state.PID != owner.PID || state.Owner != owner.Owner || !state.AllMachines || state.Root != owner.Root {
		return nil
	}
	s.RunningUnknown = false
	s.RunningVersion = state.Version
	return nil
}
