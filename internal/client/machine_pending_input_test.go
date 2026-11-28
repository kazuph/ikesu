package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestOfflineMachineSpaceClickKeepsLocalInputFlowing(t *testing.T) {
	t.Parallel()
	const sshReason = "ssh: exit status 255: ssh: connect to host mini port 22: Connection refused"
	for _, status := range []string{"connecting", "reconnecting", "needs attention", "awaiting approval", machine.ApprovalStopped} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			out, err := os.CreateTemp(t.TempDir(), "viewer")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			peer := endpoint.Current()
			local := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
			snapshot := &render.Input{State: model.New(t.TempDir(), 120, 45)}
			remote := &managedEndpoint{profile: machine.Profile{ID: "remote", Label: "Mini", Enabled: true}, snapshot: snapshot, status: status, reason: sshReason}
			if status == "connecting" {
				remote.reason = ""
			}
			if status == "awaiting approval" {
				remote.approval = "https://example.invalid/approval"
			}
			m := &machineClient{active: "local", order: []string{"local", "remote"}, out: out, size: tui.Winsize{Cols: 120, Rows: 45}, settings: &localSettings{}, endpoints: map[string]*managedEndpoint{
				"local":  {profile: machine.Profile{ID: "local", Enabled: true}, connection: local, snapshot: snapshot},
				"remote": remote,
			}}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			var click string
			for _, hit := range m.sidebar.Hits {
				if hit.Machine == "remote" && hit.Workspace != 0 {
					click = fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1)
					break
				}
			}
			if click == "" {
				t.Fatal("offline space was not drawn")
			}
			if err := m.input(mouseEvent(t, click)); err != nil {
				t.Fatal(err)
			}
			if m.pending != nil || m.drag != nil || m.active != "local" {
				t.Fatal("offline click started a switch or drag")
			}
			rendered, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			wantReason := remote.reason
			if wantReason == "" {
				wantReason = status
			}
			if m.regions.ToastRect.W == 0 || !strings.Contains(ansi.Strip(string(rendered)), wantReason) {
				t.Fatalf("connection reason %q missing from terminal output", wantReason)
			}
			for _, event := range []input.Event{{Kind: input.KindKey, Raw: []byte("a")}, {Kind: input.KindPaste, Text: "pasted"}} {
				if err := m.input(event); err != nil {
					t.Fatal(err)
				}
			}
			var received []byte
			for _, message := range local.queue {
				if message.Type == "input" {
					data, err := base64.StdEncoding.DecodeString(message.Data)
					if err != nil {
						t.Fatal(err)
					}
					received = append(received, data...)
				}
			}
			if string(received) != "a\x1b[200~pasted\x1b[201~" {
				t.Fatalf("input did not reach the visible endpoint immediately: %q", received)
			}
		})
	}
}

func TestMachineSwitchTimeoutKeepsConnectionReason(t *testing.T) {
	t.Parallel()
	const reason = "ssh: exit status 255: Permission denied (publickey)."
	deadline := time.Unix(100, 0)
	remote := &managedEndpoint{status: "needs attention", reason: reason}
	m := &machineClient{active: "local", pending: &pendingMachine{id: "remote", deadline: deadline}, endpoints: map[string]*managedEndpoint{"remote": remote}}
	m.advance(deadline)
	if m.pending != nil || m.active != "local" || remote.reason != reason || m.switchToast == nil || m.switchToast.Body != reason {
		t.Fatalf("timeout replaced the connection reason or left the switch pending: reason=%q toast=%+v pending=%+v", remote.reason, m.switchToast, m.pending)
	}
}

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
