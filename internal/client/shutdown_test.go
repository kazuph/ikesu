package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"github.com/kazuph/goterrace/internal/tui"
)

func shutdownSocketPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(teststate.TempDir(t), "shutdown.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptUnix()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	for _, conn := range []*net.UnixConn{client, server} {
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		t.Cleanup(func() { conn.Close() })
	}
	return server, client
}

func shutdownSnapshot(t *testing.T) msg {
	t.Helper()
	var encoder endpoint.SnapshotEncoder
	data, err := encoder.Encode(render.Input{State: model.New(t.TempDir(), 80, 24)})
	if err != nil {
		t.Fatal(err)
	}
	return msg{Type: "snapshot", Data: base64.StdEncoding.EncodeToString(data), RequestAck: true}
}

// The peer closes its real Unix socket's read half, so ACK writes get the
// kernel's EPIPE while its write half can still deliver a terminal message.
func TestMachineACKBrokenPipeDrainsRestartAndExit(t *testing.T) {
	for _, terminal := range []string{"restart", "exit", "closed_without_terminal"} {
		t.Run(terminal, func(t *testing.T) {
			server, client := shutdownSocketPair(t)
			output, err := os.CreateTemp(t.TempDir(), "terminal")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			ctx, cancel := context.WithCancel(context.Background())
			c := config.Config{}
			m := &machineClient{ctx: ctx, session: "shutdown", settings: &localSettings{config: &c}, active: "local", order: []string{"local"}, size: tui.Winsize{Cols: 80, Rows: 24}, out: output, events: make(chan endpointEvent), endpoints: map[string]*managedEndpoint{"local": {profile: machine.Profile{ID: "local", Label: "Local", Enabled: true}}}}
			defer func() { cancel(); m.workers.Wait() }()
			m.start("local", client)
			var hello msg
			if err := json.NewDecoder(server).Decode(&hello); err != nil {
				t.Fatal(err)
			}
			encoder := json.NewEncoder(server)
			if err := encoder.Encode(msg{Type: "welcome", Peer: endpoint.Current()}); err != nil {
				t.Fatal(err)
			}
			receive := func() endpointEvent {
				t.Helper()
				select {
				case event := <-m.events:
					return event
				case <-time.After(5 * time.Second):
					t.Fatal("endpoint event timed out")
					return endpointEvent{}
				}
			}
			event := receive()
			if event.connection == nil {
				t.Fatal(event.err)
			}
			m.event(event)
			if err := server.CloseRead(); err != nil {
				t.Fatal(err)
			}
			connection := m.endpoints["local"].connection
			if err := connection.writer.send(msg{Type: "rendered"}); !errors.Is(err, syscall.EPIPE) {
				t.Fatal("real socket did not produce EPIPE", err)
			}
			if err := encoder.Encode(shutdownSnapshot(t)); err != nil {
				t.Fatal(err)
			}
			event = receive()
			if event.message.Type != "snapshot" {
				t.Fatal(event.err, event.message.Type)
			}
			if _, done := m.event(event); done {
				t.Fatal("paint ended before terminal message")
			}
			select {
			case event := <-m.events:
				t.Fatal("ACK failure terminated the reader before terminal message", event.err, event.message.Type)
			case <-time.After(100 * time.Millisecond):
			}
			connection.queueMu.Lock()
			writeErr, queued := connection.writeErr, len(connection.queue)
			connection.queueMu.Unlock()
			if !isClosedRenderAck(writeErr) || queued != 0 {
				t.Fatal("ACK writer did not stop cleanly", writeErr, queued)
			}
			if err := connection.enqueue(msg{Type: "rendered"}); err != nil {
				t.Fatal(err)
			}
			connection.queueMu.Lock()
			queued = len(connection.queue)
			connection.queueMu.Unlock()
			if queued != 0 {
				t.Fatal("closed ACK writer accumulated a queue")
			}
			if terminal == "closed_without_terminal" {
				if err := server.CloseWrite(); err != nil {
					t.Fatal(err)
				}
				event = receive()
				if event.err == nil {
					t.Fatal("missing terminal message was treated as success")
				}
				if _, done := m.event(event); done || m.endpoints["local"].connection != nil || m.endpoints["local"].restarting {
					t.Fatal("unexpected EOF did not retain reconnect policy")
				}
				return
			}
			if err := encoder.Encode(msg{Type: terminal}); err != nil {
				t.Fatal(err)
			}
			event = receive()
			if event.message.Type != terminal {
				t.Fatal("terminal message lost", event.err, event.message.Type)
			}
			result, done := m.event(event)
			if terminal == "exit" {
				if !done || result != pumpDone {
					t.Fatal(result, done)
				}
			} else if done || !m.endpoints["local"].restarting || m.endpoints["local"].connection != nil {
				t.Fatal("restart did not select reconnection", done, m.endpoints["local"])
			}
		})
	}
}

func TestLegacyPumpACKBrokenPipeDrainsRestartAndExit(t *testing.T) {
	for _, terminal := range []string{"restart", "exit", "closed_without_terminal"} {
		t.Run(terminal, func(t *testing.T) {
			server, client := shutdownSocketPair(t)
			master, input, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer input.Close()
			if err := pty.Setsize(input, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			output, err := os.CreateTemp(t.TempDir(), "terminal")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			c := config.Config{}
			result := make(chan pumpResult, 1)
			go func() { result <- pump(client, input, output, func() {}, &localSettings{config: &c}) }()
			var hello msg
			if err := json.NewDecoder(server).Decode(&hello); err != nil {
				t.Fatal(err)
			}
			encoder := json.NewEncoder(server)
			if err := encoder.Encode(msg{Type: "welcome", Peer: endpoint.Current()}); err != nil {
				t.Fatal(err)
			}
			if err := server.CloseRead(); err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(shutdownSnapshot(t)); err != nil {
				t.Fatal(err)
			}
			if terminal == "closed_without_terminal" {
				if err := server.CloseWrite(); err != nil {
					t.Fatal(err)
				}
			} else if err := encoder.Encode(msg{Type: terminal}); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-result:
				want := pumpDone
				if terminal == "restart" {
					want = pumpRestart
				}
				if terminal == "closed_without_terminal" {
					want = pumpClosed
				}
				if got != want {
					t.Fatal("terminal message lost after ACK EPIPE", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("pump did not consume terminal message")
			}
		})
	}
}

func TestACKHalfCloseDoesNotSuppressInputOrTerminalOutputErrors(t *testing.T) {
	server, client := shutdownSocketPair(t)
	if err := server.CloseRead(); err != nil {
		t.Fatal(err)
	}
	peer := endpoint.Current()
	w := &writer{conn: client, available: peer.Generation, methods: peer.Methods}
	if err := w.send(msg{Type: "input", Data: "eA=="}); !errors.Is(err, syscall.EPIPE) || isClosedRenderAck(err) {
		t.Fatal("input failure was suppressed", err)
	}
	reader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	defer output.Close()
	err = w.firstSnapshot(output, []byte("frame"), &windowTitleState{}, render.Input{State: model.New(t.TempDir(), 80, 24)}, &render.Output{}, 0)
	if !errors.Is(err, syscall.EPIPE) || isClosedRenderAck(err) {
		t.Fatal("terminal output failure was suppressed", err)
	}
}
