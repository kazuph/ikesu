// Package herdrimport reads observed herdr v5 data into the existing gtx
// session contract. It does not execute saved launch commands.
package herdrimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type saved struct {
	Version             int         `json:"version"`
	Workspaces          []workspace `json:"workspaces"`
	Active              *int        `json:"active"`
	Selected            *int        `json:"selected"`
	SidebarWidth        *int        `json:"sidebar_width"`
	SidebarSectionSplit float64     `json:"sidebar_section_split"`
	CollapsedSpaceKeys  []string    `json:"collapsed_space_keys"`
	CollapsedSections   []string    `json:"collapsed_workspace_sections"`
}
type workspace struct {
	ID            string  `json:"id"`
	CustomName    *string `json:"custom_name"`
	Section       string  `json:"section"`
	Cwd           string  `json:"identity_cwd"`
	TabNumbers    []int   `json:"public_tab_numbers"`
	NextTabNumber int     `json:"next_public_tab_number"`
	Tabs          []tab   `json:"tabs"`
	ActiveTab     *int    `json:"active_tab"`
}
type tab struct {
	CustomName *string         `json:"custom_name"`
	Layout     tree            `json:"layout"`
	Order      []int           `json:"pane_order"`
	Panes      map[string]pane `json:"panes"`
	Zoomed     bool            `json:"zoomed"`
	Focused    int             `json:"focused"`
	RootPane   int             `json:"root_pane"`
}
type tree struct {
	Pane  *int   `json:"Pane"`
	Split *split `json:"Split"`
}
type split struct {
	Direction string   `json:"direction"`
	Ratio     *float64 `json:"ratio"`
	First     *tree    `json:"first"`
	Second    *tree    `json:"second"`
}
type ref struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}
type pane struct {
	Number       int      `json:"global_pane_number"`
	Cwd          string   `json:"cwd"`
	Label        string   `json:"label"`
	Title        string   `json:"title"`
	AgentName    string   `json:"agent_name"`
	ManagedAgent string   `json:"managed_agent_kind"`
	LaunchArgv   []string `json:"launch_argv"`
	Session      *ref     `json:"agent_session"`
}
type ledger struct {
	Entries map[string]ledgerRow `json:"entries"`
}
type ledgerRow struct {
	Pane       int    `json:"pane_id"`
	Terminal   string `json:"terminal_id"`
	Workspace  string `json:"workspace_id"`
	Tab        string `json:"tab_id"`
	Cwd        string `json:"cwd"`
	Agent      string `json:"agent"`
	ID         string `json:"session_id"`
	ObservedAt uint64 `json:"observed_at"`
	Source     string `json:"source"`
	Title      string `json:"title"`
}
type snapshot struct {
	SavedAt string        `json:"saved_at"`
	Agents  []snapshotRow `json:"agents"`
}
type snapshotRow struct {
	Pane         string   `json:"pane_id"`
	ShortPane    string   `json:"short_pane_id"`
	Workspace    string   `json:"workspace_id"`
	Cwd          string   `json:"cwd"`
	Agent        string   `json:"agent"`
	ID           string   `json:"session_id"`
	LastActivity string   `json:"last_activity"`
	LaunchFlags  []string `json:"launch_flags"`
}

func decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
func read(dir, name string, dst any, optional bool) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if optional && os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if err := decode(data, dst); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}
