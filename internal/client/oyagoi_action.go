package client

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
)

func (m *machineClient) watchOyagoi(ctx context.Context) (func(), error) {
	stop, err := watchMachineCatalog(ctx, m.oyagoiStore.path, func(ctx context.Context, err error) { m.post(ctx, endpointEvent{oyagoiChanged: true, err: err}) })
	if err != nil {
		return nil, err
	}
	if err := m.refreshOyagoi(); err != nil {
		m.notify(err.Error())
	}
	return stop, nil
}

type oyagoiCacheKey struct {
	selection                oyagoiSelection
	snapshot                 *render.Input
	connection               *endpointConnection
	attempt, snapshotAttempt uint64
	session, label           string
}

type oyagoiCache struct {
	key     oyagoiCacheKey
	sidebar render.Oyagoi
	label   string
}

func (m *machineClient) resolveOyagoi() *oyagoiCache {
	if m.oyagoi == nil {
		m.oyagoiCache = nil
		return &oyagoiCache{}
	}
	e := m.endpoints[m.oyagoi.Machine]
	key := oyagoiCacheKey{selection: *m.oyagoi, session: m.selectionSession(m.oyagoi.Machine)}
	if e != nil {
		key.snapshot, key.connection, key.attempt, key.snapshotAttempt, key.label = e.snapshot, e.connection, e.attempt, e.snapshotAttempt, e.profile.Label
	}
	if m.oyagoiCache != nil && m.oyagoiCache.key == key {
		return m.oyagoiCache
	}
	result := &oyagoiCache{key: key}
	result.sidebar.Machine, result.sidebar.Workspace, result.sidebar.Offline = m.oyagoi.Machine, m.oyagoi.Workspace, true
	result.sidebar.Collapsed = m.oyagoi.Collapsed
	name, label := fmt.Sprintf("s%d", m.oyagoi.Workspace), m.oyagoi.Machine
	if key.label != "" {
		label = key.label
	}
	if e != nil && e.connection != nil && e.snapshotAttempt == e.attempt && e.snapshot != nil && e.snapshot.State != nil && key.session == m.oyagoi.Session {
		for i, ws := range e.snapshot.State.Workspaces {
			if ws.ID == m.oyagoi.Workspace && ws.Generation == m.oyagoi.Generation {
				result.sidebar.HideOriginal, result.sidebar.Offline = true, false
				result.sidebar.WorkspaceIndex = &i
				name = e.snapshot.WorkspaceNames[i]
				if name == "" || ws.CustomName {
					name = render.WorkspaceDisplayName(ws, e.snapshot.Panes)
				}
				break
			}
		}
	}
	result.label = name + " · " + label
	if result.sidebar.Offline {
		result.label += " (offline)"
	}
	m.oyagoiCache = result
	return result
}

func (m *machineClient) oyagoiForSidebar() *render.Oyagoi { return &m.resolveOyagoi().sidebar }

func (m *machineClient) openOyagoiMenu(id string, x, y int) error {
	if m.oyagoi == nil || m.oyagoi.Machine != id {
		return fmt.Errorf("HQ selection changed")
	}
	e := m.endpoints[id]
	if e == nil || e.connection == nil {
		return fmt.Errorf("HQ workspace is offline")
	}
	return e.connection.enqueue(msg{Type: "workspace_menu", Workspace: m.oyagoi.Workspace, WorkspaceGeneration: m.oyagoi.Generation, MenuX: x, MenuY: y})
}

type oyagoiProjection struct {
	workspace         int
	generation, label string
	online            bool
	collapsed         bool
}

func (m *machineClient) selectOyagoi() error {
	if m.oyagoi == nil {
		return fmt.Errorf("HQ selection changed")
	}
	e := m.endpoints[m.oyagoi.Machine]
	if e == nil || e.connection == nil || e.snapshot == nil || e.snapshot.State == nil || e.snapshotAttempt != e.attempt || m.selectionSession(m.oyagoi.Machine) != m.oyagoi.Session {
		return fmt.Errorf("HQ workspace is offline")
	}
	valid := false
	for _, ws := range e.snapshot.State.Workspaces {
		if ws.ID == m.oyagoi.Workspace && ws.Generation == m.oyagoi.Generation {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("HQ workspace changed")
	}
	m.pending = &pendingMachine{id: m.oyagoi.Machine, workspace: m.oyagoi.Workspace, generation: m.oyagoi.Generation, deadline: time.Now().Add(machineSwitchWait)}
	m.requestPending()
	return nil
}

func (m *machineClient) syncOyagoi(id string) error {
	e := m.endpoints[id]
	if e == nil || e.connection == nil {
		return nil
	}
	if m.oyagoi == nil && e.oyagoiProjection == nil {
		return nil
	}
	selected := m.oyagoi != nil && m.oyagoi.Machine == id && m.oyagoi.Session == m.selectionSession(id)
	if !endpoint.Allows(e.connection.writer.available, endpoint.MachineGeneration, e.connection.writer.methods, "oyagoi_view") {
		if selected {
			return fmt.Errorf("endpoint_action_unsupported: HQ view")
		}
		return nil
	}
	message := msg{Type: "oyagoi_view"}
	if m.oyagoi != nil {
		message.OyagoiCollapsed = m.oyagoi.Collapsed
	}
	if selected {
		message.Workspace, message.WorkspaceGeneration = m.oyagoi.Workspace, m.oyagoi.Generation
	} else if m.oyagoi != nil {
		projection := m.resolveOyagoi()
		message.OyagoiLabel = projection.label
		message.OyagoiOnline = !projection.sidebar.Offline
	}
	projection := oyagoiProjection{message.Workspace, message.WorkspaceGeneration, message.OyagoiLabel, message.OyagoiOnline, message.OyagoiCollapsed}
	if e.oyagoiProjection != nil && *e.oyagoiProjection == projection {
		return nil
	}
	if err := e.connection.enqueue(message); err != nil {
		return err
	}
	e.oyagoiProjection = &projection
	return nil
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
		m.oyagoi = nil
		for id := range m.endpoints {
			if syncErr := m.syncOyagoi(id); syncErr != nil {
				m.notify(syncErr.Error())
			}
		}
		return fmt.Errorf("HQ settings: %w", err)
	}
	m.oyagoi = selection
	for id := range m.endpoints {
		if err := m.syncOyagoi(id); err != nil {
			m.notify(err.Error())
		}
	}
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
	if e.connection == nil {
		return nil
	}
	if e.oyagoiCheck != nil && *e.oyagoiCheck == *selected {
		return nil
	}
	if err := e.connection.enqueue(msg{Type: "oyagoi_check", Workspace: selected.Workspace, WorkspaceGeneration: selected.Generation}); err != nil {
		return err
	}
	copy := *selected
	e.oyagoiCheck = &copy
	return nil
}

func (m *machineClient) confirmOyagoiSnapshot(id string, message msg) error {
	e := m.endpoints[id]
	if e == nil || e.oyagoiCheck == nil || message.OyagoiExists == nil {
		return nil
	}
	checked := *e.oyagoiCheck
	if checked.Workspace != message.Workspace || checked.Generation != message.WorkspaceGeneration {
		return nil
	}
	e.oyagoiCheck = nil
	if *message.OyagoiExists {
		return nil
	}
	if _, err := m.oyagoiStore.clearIf(checked); err != nil {
		return err
	}
	return m.refreshOyagoi()
}
