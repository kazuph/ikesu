package client

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/render"
)

func (m *machineClient) oyagoiForSidebar() *render.Oyagoi {
	result := &render.Oyagoi{}
	if m.oyagoi == nil {
		return result
	}
	result.Machine, result.Workspace = m.oyagoi.Machine, m.oyagoi.Workspace
	e := m.endpoints[m.oyagoi.Machine]
	result.Offline = e == nil || e.connection == nil || e.snapshot == nil || m.selectionSession(m.oyagoi.Machine) != m.oyagoi.Session
	if e != nil && e.snapshot != nil && e.snapshot.State != nil && m.selectionSession(m.oyagoi.Machine) == m.oyagoi.Session {
		for _, ws := range e.snapshot.State.Workspaces {
			if ws.ID == m.oyagoi.Workspace && ws.Generation == m.oyagoi.Generation {
				result.HideOriginal = true
				break
			}
		}
	}
	if !result.HideOriginal {
		result.Offline = true
	}
	return result
}

func (m *machineClient) oyagoiAction(id, action string) error {
	parts := strings.Split(action, ":")
	if len(parts) != 4 || parts[0] != "oyagoi" || (parts[1] != "set" && parts[1] != "unset") {
		return fmt.Errorf("invalid HQ action")
	}
	workspace, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid HQ workspace")
	}
	e := m.endpoints[id]
	if e == nil || e.snapshot == nil || e.snapshot.State == nil || e.snapshotAttempt != e.attempt {
		return fmt.Errorf("HQ workspace is unavailable")
	}
	valid := false
	for _, ws := range e.snapshot.State.Workspaces {
		if ws.ID == workspace && ws.Generation != "" && ws.Generation == parts[3] {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("HQ workspace changed")
	}
	selection := oyagoiSelection{Machine: id, Session: m.selectionSession(id), Workspace: workspace, Generation: parts[3]}
	if parts[1] == "set" {
		err = m.oyagoiStore.set(&selection)
	} else {
		_, err = m.oyagoiStore.clearIf(selection)
	}
	if err != nil {
		return err
	}
	return m.refreshOyagoi()
}

func (m *machineClient) refreshOyagoi() error {
	selection, err := m.oyagoiStore.load()
	if err != nil {
		return fmt.Errorf("HQ settings: %w", err)
	}
	m.oyagoi = selection
	return nil
}

func (m *machineClient) validateOyagoiSnapshot(id string) error {
	selected := m.oyagoi
	e := m.endpoints[id]
	if selected == nil || selected.Machine != id || e == nil || selected.Session != m.selectionSession(id) || e.snapshot == nil || e.snapshot.State == nil {
		return nil
	}
	for _, ws := range e.snapshot.State.Workspaces {
		if ws.Generation == "" {
			return nil
		}
		if ws.ID == selected.Workspace && ws.Generation == selected.Generation {
			return nil
		}
	}
	if _, err := m.oyagoiStore.clearIf(*selected); err != nil {
		return err
	}
	return m.refreshOyagoi()
}
