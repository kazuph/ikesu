package client

import (
	"fmt"
	"strconv"
	"strings"
)

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
