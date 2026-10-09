// Package plugin hosts declarative executable workflow packages.
package plugin

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Build struct {
	Command   []string `toml:"command" json:"command"`
	Platforms []string `toml:"platforms" json:"platforms,omitempty"`
}
type Action struct {
	ID          string   `toml:"id" json:"id"`
	Title       string   `toml:"title" json:"title"`
	Description string   `toml:"description" json:"description,omitempty"`
	Contexts    []string `toml:"contexts" json:"contexts,omitempty"`
	Command     []string `toml:"command" json:"command"`
	Platforms   []string `toml:"platforms" json:"platforms,omitempty"`
}
type Event struct {
	On        string   `toml:"on" json:"on"`
	Command   []string `toml:"command" json:"command"`
	Platforms []string `toml:"platforms" json:"platforms,omitempty"`
}
type Pane struct {
	ID          string   `toml:"id" json:"id"`
	Title       string   `toml:"title" json:"title"`
	Description string   `toml:"description" json:"description,omitempty"`
	Command     []string `toml:"command" json:"command"`
	Platforms   []string `toml:"platforms" json:"platforms,omitempty"`
	Placement   string   `toml:"placement" json:"placement"`
	Width       any      `toml:"width" json:"width,omitempty"`
	Height      any      `toml:"height" json:"height,omitempty"`
}
type LinkHandler struct {
	ID        string   `toml:"id" json:"id"`
	Title     string   `toml:"title" json:"title"`
	Pattern   string   `toml:"pattern" json:"pattern"`
	Action    string   `toml:"action" json:"action"`
	Platforms []string `toml:"platforms" json:"platforms,omitempty"`
}
type Manifest struct {
	ID           string        `toml:"id" json:"plugin_id"`
	Name         string        `toml:"name" json:"name"`
	Version      string        `toml:"version" json:"version"`
	MinVersion   string        `toml:"min_gtx_version" json:"min_gtx_version"`
	Description  string        `toml:"description" json:"description,omitempty"`
	Platforms    []string      `toml:"platforms" json:"platforms,omitempty"`
	Build        []Build       `toml:"build" json:"build,omitempty"`
	Actions      []Action      `toml:"actions" json:"actions,omitempty"`
	Events       []Event       `toml:"events" json:"events,omitempty"`
	Panes        []Pane        `toml:"panes" json:"panes,omitempty"`
	LinkHandlers []LinkHandler `toml:"link_handlers" json:"link_handlers,omitempty"`
	Startup      []Build       `toml:"startup" json:"startup,omitempty"`
}

var packageID = regexp.MustCompile(`^[A-Za-z0-9.:_-]+$`)
var localID = regexp.MustCompile(`^[A-Za-z0-9:_-]+$`)

func ReadManifest(path, version string) (Manifest, string, error) {
	var m Manifest
	path, err := filepath.Abs(path)
	if err != nil {
		return m, "", err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return m, "", err
	}
	if fi.IsDir() {
		path = filepath.Join(path, "gtx-plugin.toml")
	}
	if _, err = toml.DecodeFile(path, &m); err != nil {
		return m, "", err
	}
	if !packageID.MatchString(m.ID) || m.ID == "." || m.ID == ".." {
		return m, "", fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return m, "", fmt.Errorf("name is required")
	}
	if _, err = parseVersion(m.Version); err != nil {
		return m, "", fmt.Errorf("version: %w", err)
	}
	minimum, err := parseVersion(m.MinVersion)
	if err != nil {
		return m, "", fmt.Errorf("min_gtx_version: %w", err)
	}
	current, err := parseVersion(version)
	if err != nil {
		return m, "", err
	}
	for i := range minimum {
		if minimum[i] > current[i] {
			return m, "", fmt.Errorf("requires gtx %s; running %s", m.MinVersion, version)
		}
		if minimum[i] < current[i] {
			break
		}
	}
	if err = validatePlatforms(m.Platforms); err != nil {
		return m, "", err
	}
	check := func(command, platforms []string) error {
		if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
			return fmt.Errorf("command must be a nonempty argv array")
		}
		for _, arg := range command {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("command contains NUL")
			}
		}
		return validatePlatforms(platforms)
	}
	for _, b := range append(append([]Build{}, m.Build...), m.Startup...) {
		if err = check(b.Command, b.Platforms); err != nil {
			return m, "", err
		}
	}
	seen := map[string]bool{}
	identifier := func(id, title string) error {
		if !localID.MatchString(id) || seen[id] || strings.TrimSpace(title) == "" {
			return fmt.Errorf("invalid or duplicate entrypoint %q, or missing title", id)
		}
		seen[id] = true
		return nil
	}
	for _, a := range m.Actions {
		if err = identifier(a.ID, a.Title); err != nil {
			return m, "", err
		}
		if err = check(a.Command, a.Platforms); err != nil {
			return m, "", err
		}
		for _, c := range a.Contexts {
			switch c {
			case "global", "workspace", "tab", "pane", "selection":
			default:
				return m, "", fmt.Errorf("invalid action context %q", c)
			}
		}
	}
	actions := seen
	seen = map[string]bool{}
	for i := range m.Panes {
		p := &m.Panes[i]
		if err = identifier(p.ID, p.Title); err != nil {
			return m, "", err
		}
		if err = check(p.Command, p.Platforms); err != nil {
			return m, "", err
		}
		if p.Placement == "" {
			p.Placement = "overlay"
		}
		switch p.Placement {
		case "overlay", "popup", "split", "tab", "zoomed":
		default:
			return m, "", fmt.Errorf("invalid placement %q", p.Placement)
		}
		for _, size := range []any{p.Width, p.Height} {
			if err = validateSize(size); err != nil {
				return m, "", err
			}
		}
	}
	for _, e := range m.Events {
		if e.On == "" {
			return m, "", fmt.Errorf("event on is required")
		}
		if err = check(e.Command, e.Platforms); err != nil {
			return m, "", err
		}
	}
	seen = map[string]bool{}
	for _, h := range m.LinkHandlers {
		if err = identifier(h.ID, h.Title); err != nil {
			return m, "", err
		}
		if !actions[h.Action] {
			return m, "", fmt.Errorf("link handler action %q not declared", h.Action)
		}
		if _, err = regexp.Compile(h.Pattern); err != nil {
			return m, "", err
		}
		if err = validatePlatforms(h.Platforms); err != nil {
			return m, "", err
		}
	}
	return m, path, nil
}
func parseVersion(s string) ([3]uint64, error) {
	var out [3]uint64
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid version %q; expected major.minor.patch", s)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("invalid version %q", s)
		}
		out[i] = n
	}
	return out, nil
}
func validatePlatforms(platforms []string) error {
	for _, p := range platforms {
		switch p {
		case "linux", "macos", "windows":
		default:
			return fmt.Errorf("invalid platform %q", p)
		}
	}
	return nil
}
func validateSize(size any) error {
	if size == nil {
		return nil
	}
	switch v := size.(type) {
	case int64:
		if v > 0 {
			return nil
		}
	case string:
		if strings.HasSuffix(v, "%") {
			n, e := strconv.ParseUint(strings.TrimSuffix(v, "%"), 10, 16)
			if e == nil && n > 0 && n <= 100 {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid popup dimension %v", size)
}
