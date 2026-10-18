package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestPendingMachineKeysFollowDestinationRenderedAck(t *testing.T) {
	reader, writerFile, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writerFile.Close()
	peer := endpoint.Current()
	target := &endpointConnection{writer: &writer{conn: writerFile, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	old := &endpointConnection{writer: &writer{conn: writerFile, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	output, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	state := model.New(t.TempDir(), 120, 45)
	snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
	m := &machineClient{active: "local", order: []string{"local", "remote"}, out: output, settings: &localSettings{}, size: tui.Winsize{Cols: 120, Rows: 45}, regions: &render.Output{}, endpoints: map[string]*managedEndpoint{
		"local":  {profile: machine.Profile{ID: "local", Enabled: true}, connection: old, snapshot: snapshot},
		"remote": {profile: machine.Profile{ID: "remote", Enabled: true}, connection: target, snapshot: snapshot, epoch: 1},
	}, pending: &pendingMachine{id: "remote", workspace: state.ActiveWorkspace().ID, deadline: time.Now().Add(machineSwitchWait)}}
	for _, raw := range []string{"a", "\r"} {
		if err := m.input(input.Event{Kind: input.KindKey, Raw: []byte(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(old.queue) != 0 || len(target.queue) != 0 {
		t.Fatal("keys escaped before the destination was ready")
	}
	m.finishSwitch()
	if m.active != "remote" || m.pending != nil {
		t.Fatal("ready destination was not selected")
	}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- target.writeQueued(ctx) }()
	reader.SetReadDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(reader)
	var received []byte
	seenAck := false
	for i := 0; i < 4; i++ {
		var message msg
		if err := decoder.Decode(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type == "rendered" {
			seenAck = true
		}
		if message.Type == "input" {
			if !seenAck {
				t.Fatal("keys preceded the destination render acknowledgement")
			}
			data, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil {
				t.Fatal(err)
			}
			received = append(received, data...)
		}
	}
	if string(received) != "a\r" {
		t.Fatalf("destination bytes=%q", received)
	}
	cancel()
	<-done
	for _, message := range old.queue {
		if message.Type == "input" {
			t.Fatal("old endpoint received destination keys")
		}
	}
}

func TestPendingMachineInputLimitsExpireWithoutChangingSelection(t *testing.T) {
	start := time.Unix(100, 0)
	p := &pendingMachine{id: "remote", deadline: start.Add(machineSwitchWait)}
	m := &machineClient{active: "local", pending: p, endpoints: map[string]*managedEndpoint{"local": {}, "remote": {}}}
	for i := 0; i < pendingInputLimit+1; i++ {
		if !m.holdPendingInput(input.Event{Kind: input.KindKey, Raw: []byte("x")}, start) {
			t.Fatal("pending key was not held")
		}
	}
	if len(p.keys) != pendingInputLimit {
		t.Fatalf("held=%d want %d", len(p.keys), pendingInputLimit)
	}
	if !m.nextDeadline().Equal(start.Add(pendingInputWait)) {
		t.Fatal("input expiry is not scheduled separately from connection timeout")
	}
	m.advance(start.Add(pendingInputWait - time.Nanosecond))
	if len(p.keys) != pendingInputLimit {
		t.Fatal("input expired early")
	}
	m.advance(start.Add(pendingInputWait))
	if len(p.keys) != 0 || m.pending == nil || m.active != "local" {
		t.Fatal("input expiry changed selection or retained keys")
	}
	if !m.holdPendingInput(input.Event{Kind: input.KindPaste, Text: "late"}, start.Add(pendingInputWait)) || len(p.keys) != 0 {
		t.Fatal("expired paste reached the old endpoint or was retained")
	}
	m.advance(start.Add(machineSwitchWait))
	if m.pending != nil || m.active != "local" || len(m.held) != 0 {
		t.Fatal("failed switch leaked held input")
	}
}

func TestPendingMachineInputWaitsThroughDisconnectAndOwnsBytes(t *testing.T) {
	start := time.Now()
	p := &pendingMachine{id: "remote", deadline: start.Add(machineSwitchWait)}
	m := &machineClient{active: "local", pending: p, endpoints: map[string]*managedEndpoint{"local": {}, "remote": {}}}
	raw := []byte("a")
	m.holdPendingInput(input.Event{Kind: input.KindKey, Raw: raw}, start)
	raw[0] = 'z'
	m.finishSwitch()
	if m.pending != p || m.active != "local" || string(p.keys[0].Raw) != "a" {
		t.Fatal("disconnected destination consumed keys or held borrowed bytes")
	}
	m.holdPendingInput(input.Event{Kind: input.KindKey, Raw: []byte("b")}, start.Add(pendingInputWait))
	if len(p.keys) != 0 || len(m.held) != 0 {
		t.Fatal("disconnected destination retained expired keys")
	}
}
