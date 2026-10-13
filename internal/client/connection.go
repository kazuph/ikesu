package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/tui"
)

// Each connection keeps its own decoder and writer. Only the terminal
// supervisor reads stdin and decides which endpoint receives that input.
type endpointConnection struct {
	stream    io.ReadWriteCloser
	reader    *bufio.Reader
	writer    *writer
	snapshots endpoint.SnapshotDecoder
	queueMu   sync.Mutex
	queue     []msg
	writeErr  error
	wake      chan struct{}
}

func connectEndpoint(stream io.ReadWriteCloser, remote bool, size tui.Winsize, settings *localSettings) (*endpointConnection, error) {
	peer := endpoint.Current()
	var visible *bool
	if remote {
		peer = endpoint.MachinePeer()
		hidden := false
		visible = &hidden
	}
	c := &endpointConnection{stream: stream, reader: bufio.NewReader(stream), writer: &writer{conn: stream}, wake: make(chan struct{}, 1)}
	hello := msg{Peer: peer, Type: "hello", Cols: size.Cols, Rows: size.Rows, Xpixel: size.Xpixel, Ypixel: size.Ypixel, Visible: visible,
		Config: settings.config, ConfigWarning: settings.warning, Preferences: settings.preferences}
	if err := c.writer.send(hello); err != nil {
		return nil, err
	}
	welcome, err := c.read()
	if err != nil {
		return nil, err
	}
	if welcome.Type == "error" {
		return nil, fmt.Errorf("%s", welcome.Reason)
	}
	if welcome.Type != "welcome" {
		return nil, fmt.Errorf("endpoint_generation_unsupported: no endpoint welcome")
	}
	available, err := endpoint.Negotiate(peer, welcome.Peer)
	if err != nil {
		return nil, err
	}
	if remote {
		for _, method := range []string{"visibility", "workspace_select"} {
			if !endpoint.Allows(available, endpoint.MachineGeneration, welcome.Methods, method) {
				return nil, fmt.Errorf("endpoint_action_unsupported: %s", method)
			}
		}
	}
	c.writer.available, c.writer.methods = available, welcome.Methods
	if ssh, ok := stream.(*sshEndpoint); ok {
		ssh.connected()
	}
	return c, nil
}

func (c *endpointConnection) read() (msg, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return msg{}, err
	}
	var m msg
	if err := json.Unmarshal(line, &m); err != nil {
		return msg{}, fmt.Errorf("invalid endpoint message: %w", err)
	}
	return m, nil
}

func (c *endpointConnection) Close() error { return c.stream.Close() }
