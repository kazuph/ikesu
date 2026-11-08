package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/machine"
	"io"
	"net"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
)

func (m *machineClient) post(ctx context.Context, event endpointEvent) bool {
	select {
	case m.events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *machineClient) start(id string, supplied io.ReadWriteCloser) {
	if id == "local" {
		m.focusKnown = false
		m.lastClientView = nil
		m.retirePendingGraphicsFile()
	}
	e := m.endpoints[id]
	if e.cancel != nil {
		e.cancel()
	}
	e.attempt++
	attempt := e.attempt
	e.connection = nil
	if e.reason == "" {
		e.status = "connecting"
	} else {
		e.status = "reconnecting"
	}
	ctx, cancel := context.WithCancel(m.ctx)
	e.cancel = cancel
	e.ctx = ctx
	settings := *m.settings
	configRevision := m.configRevision
	size := m.size
	selection := m.reconnectSelection(id)
	profile := e.profile
	session := m.session
	// Background Local reconnects also require hidden-viewer admission.
	hidden := id != "local" || (m.active != "local" && (m.pending == nil || m.pending.id != "local"))
	fileTransport := graphicsFileTransport(m.in, m.out, id != "local")
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()
		stream := supplied
		var err error
		if stream == nil {
			if id == "local" {
				stream, err = dialClient(api.SessionDir(session))
			} else {
				stream, err = openSSHEndpoint(profile, func(url string) { m.post(ctx, endpointEvent{id: id, attempt: attempt, approval: url}) })
			}
		}
		if err != nil {
			m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
			return
		}
		closed := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				stream.Close()
			case <-closed:
			}
		}()
		defer func() { stream.Close(); close(closed) }()
		var firstUpgradeConn net.Conn
		if id == "local" && settings.upgradeState != nil && !settings.upgradeState.Rollback {
			firstUpgradeConn, _ = stream.(net.Conn)
			if firstUpgradeConn != nil {
				if err := firstUpgradeConn.SetDeadline(time.Now().Add(attachTimeout)); err != nil {
					m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
					return
				}
			}
		}
		connection, err := connectEndpoint(stream, hidden, size, &settings, fileTransport, selection)
		if err != nil {
			m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
			return
		}
		if firstUpgradeConn != nil {
			if err := firstUpgradeConn.SetDeadline(time.Time{}); err != nil {
				m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
				return
			}
		}
		writing := make(chan struct{})
		go func() {
			defer close(writing)
			if err := connection.writeQueued(ctx); err != nil && ctx.Err() == nil {
				// The server can close its read half before the queued restart/exit.
				// Stop ACK writes, but let the reader consume that message or EOF.
				if isClosedRenderAck(err) {
					return
				}
				m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
				cancel()
			}
		}()
		defer func() { cancel(); stream.Close(); <-writing }()
		if !m.post(ctx, endpointEvent{id: id, attempt: attempt, connection: connection, configRevision: configRevision}) {
			return
		}
		for {
			message, err := connection.read()
			if !m.post(ctx, endpointEvent{id: id, attempt: attempt, message: message, err: err}) || err != nil {
				return
			}
			if m.trace != nil && message.snapshot != nil {
				m.trace.Event("surface.received", " endpoint=", id, " generation=", attempt, " boot_id=", message.traceBoot, " revision=", message.snapshot.SurfaceRevision, " bytes=", message.traceBytes, " images=", hasNativeGraphics(*message.snapshot) || hasTraceImages(*message.snapshot), " text_only=", message.traceTextOnly)
			}
		}
	}()
}

func needsAttention(reason string) bool {
	lower := strings.ToLower(reason)
	for _, text := range []string{"permission denied", "host key", "host identification", "endpoint_generation_", "endpoint_action_unsupported", "protocol_", "authentication failed"} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func (m *machineClient) event(event endpointEvent) (pumpResult, bool) {
	if event.oyagoiChanged {
		if event.err != nil {
			m.notify("HQ settings watch: " + event.err.Error())
		} else if err := m.refreshOyagoi(); err != nil {
			m.notify(err.Error())
		}
		_ = m.paint()
		return 0, false
	}
	if event.formUpdate != nil {
		event.formUpdate()
		_ = m.paint()
		return 0, false
	}
	if event.catalogChanged {
		if event.err != nil {
			m.notify("machine catalog watch: " + event.err.Error())
		} else if err := m.refreshMachineCatalog(); err != nil {
			m.notify(err.Error())
		}
		return 0, false
	}
	e := m.endpoints[event.id]
	if e == nil || e.attempt != event.attempt {
		return 0, false
	}
	if event.upgrade != nil {
		e.upgradePending = false
		result := event.upgrade
		if result.err != nil {
			logUpgradeFailure(m.session, m.clientVersion, result.identity, result.err)
			if result.identity.Generation != "" {
				m.failedUpgrades[result.identity] = true
			}
			m.notify("upgrade: " + result.err.Error())
			_ = m.paint()
			return 0, false
		}
		if result.candidate == nil || m.failedUpgrades[result.identity] {
			return 0, false
		}
		if e.restarting {
			logUpgradeFailure(m.session, m.clientVersion, result.identity, upgradeWait("local input is waiting for reconnection before client upgrade"))
			m.deferUpgrade()
			return 0, false
		}
		if err := result.candidate.executable.recheck(); err != nil {
			logUpgradeFailure(m.session, m.clientVersion, result.identity, err)
			m.failedUpgrades[result.identity] = true
			m.notify("upgrade: " + err.Error())
			_ = m.paint()
			return 0, false
		}
		result.candidate.state.Active = m.active
		result.candidate.state.Selections = make(map[string]upgradeSelection)
		for id, endpoint := range m.endpoints {
			if saved := endpoint.selection; saved != nil {
				result.candidate.state.Selections[id] = upgradeSelection{Target: saved.target, Session: saved.session, Value: saved.value}
			}
		}
		m.settings.upgrade = result.candidate
		return pumpUpgrade, true
	}
	if event.approval != "" {
		e.status = "awaiting approval"
		e.approval = event.approval
		e.reason = event.approval
		if err := machine.OpenApproval(e.profile.ID, event.approval); err != nil {
			e.reason = event.approval + "; browser: " + err.Error()
		}
		_ = m.paint()
		return 0, false
	}
	if event.err != nil {
		if event.id == "local" && m.settings.upgradeState != nil && !m.settings.upgradeState.Rollback && e.connection == nil {
			m.settings.upgradeState.Reason = event.err.Error()
			return pumpRollback, true
		}
		if event.id == "local" {
			m.focusKnown = false
			m.lastClientView = nil
			m.retirePendingGraphicsFile()
		}
		if event.id == "local" && e.snapshot == nil && needsAttention(event.err.Error()) {
			fmt.Fprintln(m.out, "ikesu:", event.err)
			return pumpRejected, true
		}
		if e.connection != nil {
			m.idleDisconnect(event.id, event.err)
		}
		e.connection = nil
		e.status = "reconnecting"
		if m.oyagoi != nil && m.oyagoi.Machine == event.id {
			for id := range m.endpoints {
				if err := m.syncOyagoi(id); err != nil {
					m.notify(err.Error())
				}
			}
		}
		e.reason = event.err.Error()
		if strings.HasPrefix(e.reason, "SSH approval expired:") {
			e.status = machine.ApprovalStopped
			e.retry.approvalExpired()
		} else {
			e.retry.failed(time.Now(), event.id == "local", needsAttention(e.reason))
			if e.retry.attention {
				e.status = "needs attention"
			}
		}
		_ = m.paint()
		return 0, false
	}
	if event.connection != nil {
		if event.id == "local" && m.settings.upgradeState != nil && !m.settings.upgradeState.Rollback {
			m.settings.upgradeState = nil
		}
		if event.id != "local" {
			if err := machine.ClearApproval(e.profile.ID); err != nil {
				m.notify(err.Error())
			}
		}
		e.connection = event.connection
		e.oyagoiProjection = nil
		e.oyagoiCheck = nil
		if err := m.syncOyagoi(event.id); err != nil {
			m.notify(err.Error())
		}
		if event.id != "local" && event.id == m.active {
			visible := !m.blurred
			if err := e.connection.enqueue(msg{Type: "visibility", Visible: &visible}); err != nil {
				m.fail(event.id, err)
				return 0, false
			}
		}
		e.processedInput = 0
		if event.configRevision != m.configRevision {
			if err := e.connection.enqueue(msg{Type: "view_config", Config: m.settings.config, Preferences: m.settings.preferences}); err != nil {
				m.notify(e.profile.Label + ": " + err.Error())
			}
		}
		// A new viewer starts with the base request. Its current pane may
		// subsequently request more flags through the output channel.
		e.hostKbdFlags = 7
		if event.id == m.active {
			if err := m.syncKeyboardOutput(e); err != nil {
				return pumpDone, true
			}
		}
		e.status = "online"
		e.reason = ""
		e.approval = ""
		e.retry.connected(time.Now())
		if event.id == "local" {
			m.configureMachineDisplay()
			if (e.restarting && !e.resumeInput) || e.upgradePending {
				m.checkUpgrade(event.attempt)
			}
		}
		if m.pending != nil && m.pending.id == event.id {
			m.requestPending()
		}
		return 0, false
	}
	message := event.message
	switch message.Type {
	case "oyagoi_check_result":
		if err := m.confirmOyagoiSnapshot(event.id, message); err != nil {
			m.notify(err.Error())
		}
		_ = m.paint()
	case "decisions":
		focused := ""
		lastInput := time.Now()
		if event.id == m.active && e.snapshot != nil && e.snapshot.State != nil {
			focused = fmt.Sprintf("p%d", e.snapshot.State.FocusedPane())
			if m.approvalCanOpen(e) {
				lastInput = e.approvalInput[focused]
			}
		}
		e.guardApproval.update(message.Decisions, focused, time.Now(), lastInput)
		_ = m.paint()
	case "decision_answer_result":
		if message.Reason != "" {
			e.guardApproval.waiting = false
			m.notify(message.Reason)
			_ = m.paint()
		}
	case "snapshot":
		if message.snapshot == nil {
			m.fail(event.id, errors.New("snapshot was not decoded by endpoint reader"))
			break
		}
		input := *message.snapshot
		if event.id != "local" && hasNativeGraphics(input) {
			m.fail(event.id, errors.New("native graphics require a local file transfer"))
			break
		}
		e.snapshot = message.snapshot
		e.snapshotAttempt = e.attempt
		if err := m.validateOyagoiSnapshot(event.id); err != nil {
			m.notify(err.Error())
		}
		if m.oyagoi != nil && m.oyagoi.Machine == event.id {
			for id := range m.endpoints {
				if err := m.syncOyagoi(id); err != nil {
					m.notify(err.Error())
				}
			}
		}
		if message.snapshot.Dialog != nil || message.snapshot.Menu != nil || message.snapshot.Help != nil || message.snapshot.Settings != nil || message.snapshot.Navigator != nil {
			e.guardApproval.dismiss()
		}
		if event.id == m.active && m.approvalCanOpen(e) {
			e.guardApproval.focus(fmt.Sprintf("p%d", message.snapshot.State.FocusedPane()))
		}
		e.processedInput = message.ProcessedInput
		e.boot = message.traceBoot
		e.textOnly = message.traceTextOnly
		e.epoch = message.FocusEpoch
		e.requestAck = message.RequestAck
		if m.pending != nil && m.pending.id == event.id {
			m.finishSwitch()
			if m.pending != nil && !m.pending.requested {
				m.requestPending()
			}
		}
		if err := m.replayUpgradeQueue(event.id); err != nil {
			m.fail(event.id, err)
			return 0, false
		}
		if err := m.paint(); err != nil {
			return pumpDone, true
		}

		if event.id != m.active && message.RequestAck {
			regions := render.Compose(input)
			regions.Frame = nil
			_ = e.connection.enqueue(msg{Type: "rendered", Rendered: regions, FocusEpoch: e.epoch})
		}
		if event.id == "local" && e.restarting {
			e.restarting = false
			e.resumeInput = false
			pending := e.restartInput
			e.restartInput = nil
			if len(pending) > 0 {
				if err := m.sendInput(event.id, pending); err != nil {
					m.fail(event.id, err)
				}
			}
			if e.upgradePending && !e.upgradeRetryAt.IsZero() && e.connection != nil {
				m.checkUpgrade(e.attempt)
			}
		}
	case endpoint.GraphicsFileMessage, endpoint.GraphicsRetiredMessage:
		if err := m.graphicsFileEvent(event.id, message); err != nil {
			m.fail(event.id, err)
		}
	case "notify":
		if message.Notification != nil && m.settings.config != nil {
			deliverDesktop(*message.Notification, *m.settings.config, m.session, event.id, func(err error) {
				// The notice is drawn on the client's own status line instead of
				// being written into the terminal over the panes.
				go m.post(m.ctx, endpointEvent{formUpdate: func() { m.notify("notification: " + err.Error()) }})
			})
		}
	case "output":
		if data, err := base64.StdEncoding.DecodeString(message.Data); err == nil {
			if m.keyboardOutput(e, data) {
				break
			}
			e.mouse.Written(data)
			if event.id == m.active {
				// A queued release still uses the press's terminal coordinates.
				if m.drag != nil && (string(data) == "\x1b[?1016l" || string(data) == "\x1b[?1016h") {
					break
				}
				n, _ := m.out.Write(data)
				m.mouse.Written(data[:n])
			}
		}
	case "preferences":
		if message.Preferences != nil {
			if err := m.settings.save(*message.Preferences); err != nil {
				_ = e.connection.enqueue(msg{Type: "preferences_error", Reason: err.Error()})
			} else {
				m.configureOtherEndpoints(event.id)
			}
		}
	case "machine_menu":
		if event.id != m.active {
			break
		}
		if message.MachineAction == "idle_panes" {
			m.openIdlePanes()
		} else if message.MachineAction == "oyagoi:toggle" {
			if err := m.oyagoiStore.toggleCollapsed(); err != nil {
				m.notify(err.Error())
			} else if err := m.refreshOyagoi(); err != nil {
				m.notify(err.Error())
			}
		} else if message.MachineAction == "oyagoi:select" {
			if err := m.selectOyagoi(); err != nil {
				m.notify(err.Error())
			}
		} else if strings.HasPrefix(message.MachineAction, "oyagoi:") {
			if err := m.oyagoiAction(event.id, message.MachineAction); err != nil {
				m.notify(err.Error())
			}
		} else if err := m.forms.Open(message.MachineAction, e.snapshot.State); err != nil {
			m.notify(err.Error())
		}
		_ = m.paint()
	case "idle_panes_result":
		m.idleListResult(event.id, message.IdleRequest, message.IdlePanes)
		_ = m.paint()
	case "idle_delete_result":
		m.idleDeleteResult(event.id, message.IdleRequest, message.IdleDeleteResult)
		_ = m.paint()
	case "config_edit":
		if message.ConfigEdit == nil {
			m.fail(event.id, errors.New("invalid config_edit"))
			break
		}
		m.queueConfigEdit(event.id, message)
	case "detached":
		if event.id == m.active {
			return pumpDone, true
		}
	case "exit":
		if event.id == "local" && m.active == "local" {
			return pumpDone, true
		}
		m.fail(event.id, errors.New("server exited"))
	case "restart":
		if event.id == "local" {
			e.restarting = true
		}
		m.fail(event.id, errors.New("server restarting"))
	case "error":
		if message.Reason == "endpoint_workspace_not_found" && m.pending != nil && m.pending.id == event.id && m.pending.generation != "" {
			m.pending = nil
		}
		m.notify(message.Reason)
	}
	return 0, false
}

func (m *machineClient) configureOtherEndpoints(source string) {
	for id, e := range m.endpoints {
		if id == source || e.connection == nil {
			continue
		}
		if err := e.connection.enqueue(msg{Type: "view_config", Config: m.settings.config, Preferences: m.settings.preferences}); err != nil {
			m.notify(e.profile.Label + ": " + err.Error())
		}
	}
}

func (m *machineClient) fail(id string, err error) {
	m.idleDisconnect(id, err)
	if id == "local" {
		m.retirePendingGraphicsFile()
	}
	e := m.endpoints[id]
	if e.cancel != nil {
		e.cancel()
	}
	e.connection = nil
	if m.oyagoi != nil && m.oyagoi.Machine == id {
		for target := range m.endpoints {
			if syncErr := m.syncOyagoi(target); syncErr != nil {
				m.notify(syncErr.Error())
			}
		}
	}
	e.status = "reconnecting"
	e.reason = err.Error()
	e.retry.failed(time.Now(), id == "local", needsAttention(e.reason))
}
