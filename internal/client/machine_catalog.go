package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/machine"
)

func (m *machineClient) catalogPath() string {
	return filepath.Join(api.SessionDir(m.session), "machines.json")
}

func (m *machineClient) refreshMachineCatalog() error {
	if err := m.syncMachineCatalog(); err != nil {
		return err
	}
	return m.paint()
}

func (m *machineClient) syncMachineCatalog() error {
	data, err := os.ReadFile(m.catalogPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("machine catalog: %w", err)
	}
	var profiles []machine.Profile
	if err == nil {
		if err = json.Unmarshal(data, &profiles); err != nil {
			return fmt.Errorf("machine catalog: %w", err)
		}
	}
	next := map[string]machine.Profile{}
	order := []string{"local"}
	for _, profile := range profiles {
		if profile.ID == "" || profile.ID == "local" {
			return fmt.Errorf("machine catalog: invalid saved machine ID")
		}
		if _, exists := next[profile.ID]; exists {
			return fmt.Errorf("machine catalog: duplicate saved machine ID %s", profile.ID)
		}
		next[profile.ID] = profile
		order = append(order, profile.ID)
	}
	for id, e := range m.endpoints {
		if id == "local" {
			continue
		}
		profile, exists := next[id]
		changedEndpoint := !exists || profile.Target != e.profile.Target || profile.Session != e.profile.Session
		if changedEndpoint {
			e.selection = nil
		}
		if changedEndpoint || (!profile.Enabled && (e.profile.Enabled || id == m.active)) {
			if m.pending != nil && m.pending.id == id {
				m.pending = nil
			}
			if m.drag != nil && m.drag.id == id {
				m.drag = nil
			}
			if e.cancel != nil {
				e.cancel()
			}
			m.pruneDisconnect(id)
			e.attempt++
			e.connection = nil
			e.retry = retryPolicy{}
			e.removed = !exists
			if exists {
				e.status, e.reason = "disconnected", "machine is disconnected"
				e.approval = ""
			} else {
				e.status, e.reason = "removed", "machine was removed"
			}
			m.idleDisconnect(id, errors.New(e.reason))
			if exists && !profile.Enabled && id == m.active {
				m.showLocalAfterDisconnect()
			}
			if id != m.active {
				e.snapshot = nil
			}
			if !exists && id != m.active {
				delete(m.endpoints, id)
			}
		}
	}
	m.order = order
	for _, profile := range profiles {
		e := m.endpoints[profile.ID]
		if e == nil {
			e = &managedEndpoint{profile: profile, status: "disconnected", reason: "machine is disconnected"}
			m.endpoints[profile.ID] = e
		}
		wasEnabled := e.profile.Enabled
		e.profile = profile
		e.removed = false
		if profile.Enabled && (!wasEnabled || e.status == "disconnected") {
			if m.legacyLocal() {
				e.status, e.reason = "unsupported", machineSidebarUnsupported
				continue
			}
			e.reason = ""
			e.retry = retryPolicy{}
			m.start(profile.ID, nil)
		}
		if !profile.Enabled {
			e.status = "disconnected"
		}
	}
	for id := range m.endpoints {
		if err := m.syncOyagoi(id); err != nil {
			m.notify(err.Error())
		}
	}
	return nil
}

func (m *machineClient) showLocalAfterDisconnect() {
	m.active = "local"
	m.pending = nil
	m.drag = nil
	m.tabFeedback = nil
	m.held = nil
	m.regions = nil
	local := m.endpoints["local"]
	if local.connection == nil {
		return
	}
	_ = local.connection.enqueue(resizeMessage(m.size))
	m.showMachineViewer(local)
}
