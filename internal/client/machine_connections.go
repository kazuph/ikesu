package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/render"
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
	size := m.size
	profile := e.profile
	session := m.session
	// Background Local reconnects also require hidden-viewer admission.
	hidden := id != "local" || (m.active != "local" && (m.pending == nil || m.pending.id != "local"))
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
		connection, err := connectEndpoint(stream, hidden, size, &settings)
		if err != nil {
			m.post(ctx, endpointEvent{id: id, attempt: attempt, err: err})
			return
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
		if !m.post(ctx, endpointEvent{id: id, attempt: attempt, connection: connection}) {
			return
		}
		for {
			message, err := connection.read()
			if !m.post(ctx, endpointEvent{id: id, attempt: attempt, message: message, err: err}) || err != nil {
				return
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
	if event.approval != "" {
		e.status = "awaiting approval"
		e.approval = event.approval
		e.reason = event.approval
		cmd := exec.Command("open", "--", event.approval)
		if err := cmd.Start(); err != nil {
			e.reason = event.approval + "; browser: " + err.Error()
		} else {
			go cmd.Wait()
		}
		_ = m.paint()
		return 0, false
	}
	if event.err != nil {
		if event.id == "local" && e.snapshot == nil && needsAttention(event.err.Error()) {
			fmt.Fprintln(m.out, "gtx:", event.err)
			return pumpRejected, true
		}
		e.connection = nil
		e.status = "reconnecting"
		e.reason = event.err.Error()
		if strings.HasPrefix(e.reason, "SSH approval expired:") {
			e.status = "承認待ち。クリックで再試行"
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
		e.connection = event.connection
		e.status = "online"
		e.reason = ""
		e.approval = ""
		e.retry.connected(time.Now())
		if event.id == "local" {
			m.configureMachineDisplay()
		}
		if m.pending != nil && m.pending.id == event.id {
			m.requestPending()
		}
		return 0, false
	}
	message := event.message
	switch message.Type {
	case "decisions", "decision_answer_result":
		// Older servers still send these controls; decisions are answered outside the display.
	case "snapshot":
		data, err := base64.StdEncoding.DecodeString(message.Data)
		if err != nil {
			m.fail(event.id, err)
			break
		}
		input, err := e.connection.snapshots.Decode(data)
		if err != nil {
			m.fail(event.id, err)
			break
		}
		e.snapshot = &input
		e.epoch = message.FocusEpoch
		e.requestAck = message.RequestAck
		if m.pending != nil && m.pending.id == event.id {
			m.finishSwitch()
			if m.pending != nil && !m.pending.requested {
				m.requestPending()
			}
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
			pending := e.restartInput
			e.restartInput = nil
			if len(pending) > 0 {
				if err := m.sendInput(event.id, pending); err != nil {
					m.fail(event.id, err)
				}
			}
		}
	case "notify":
		if message.Notification != nil && m.settings.config != nil {
			deliverDesktop(*message.Notification, *m.settings.config, m.session, event.id)
		}
	case "output":
		if event.id == m.active {
			if data, err := base64.StdEncoding.DecodeString(message.Data); err == nil {
				_, _ = m.out.Write(data)
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
		if err := m.forms.Open(message.MachineAction, e.snapshot.State); err != nil {
			m.notify(err.Error())
		}
		_ = m.paint()
	case "config_edit":
		if message.ConfigEdit == nil {
			m.fail(event.id, errors.New("invalid config_edit"))
			break
		}
		cfg, err := m.settings.apply(*message.ConfigEdit)
		result := msg{Type: "config_result", ConfigID: message.ConfigID}
		if err != nil {
			result.Reason = err.Error()
		} else {
			m.settings.config = &cfg
			result.Config = &cfg
			m.configureOtherEndpoints(event.id)
		}
		_ = e.connection.enqueue(result)
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
	e := m.endpoints[id]
	if e.cancel != nil {
		e.cancel()
	}
	e.connection = nil
	e.status = "reconnecting"
	e.reason = err.Error()
	e.retry.failed(time.Now(), id == "local", needsAttention(e.reason))
}
