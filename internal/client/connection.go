package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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

func connectEndpoint(stream io.ReadWriteCloser, remote bool, size tui.Winsize, settings *localSettings, fileTransport string) (*endpointConnection, error) {
	peer := endpoint.Current()
	var visible *bool
	if remote {
		peer = endpoint.MachinePeer()
		hidden := false
		visible = &hidden
	}
	peer.SurfaceCodecs = []string{endpoint.GraphicsPatchCodec}
	peer.Methods = append(peer.Methods, "control")
	if fileTransport != "" {
		peer = endpoint.WithGraphicsFiles(peer)
	}
	c := &endpointConnection{stream: stream, reader: bufio.NewReader(stream), writer: &writer{conn: stream}, wake: make(chan struct{}, 1)}
	hello := msg{Peer: peer, Type: "hello", Cols: size.Cols, Rows: size.Rows, Xpixel: size.Xpixel, Ypixel: size.Ypixel, Visible: visible,
		GraphicsFileTransport: fileTransport,
		Config:                settings.config, ConfigWarning: settings.warning, Preferences: settings.preferences}
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
	c.snapshots.GraphicsPatches = endpoint.HasGraphicsPatches(welcome.Peer)
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
	if m.Type == "snapshot" {
		data, err := base64.StdEncoding.DecodeString(m.Data)
		if err != nil {
			return msg{}, fmt.Errorf("invalid snapshot: %w", err)
		}
		in, err := c.snapshots.Decode(data)
		if err != nil {
			return msg{}, fmt.Errorf("invalid snapshot: %w", err)
		}
		if err := c.snapshots.PrepareGraphicsForDisplay(&in); err != nil {
			return msg{}, fmt.Errorf("invalid display graphics: %w", err)
		}
		m.snapshot = &in
		m.Data = ""
		if key := c.snapshots.MissingGraphicsKey; key != nil {
			data, err := json.Marshal(struct {
				Key any `json:"key"`
			}{key})
			if err != nil {
				return msg{}, err
			}
			if err := c.enqueue(msg{Type: "control", Kind: endpoint.GraphicsPatchMiss, Data: string(data)}); err != nil {
				return msg{}, err
			}
		} else if len(in.GraphicsPatches) > 0 {
			if err := c.enqueue(msg{Type: "control", Kind: endpoint.GraphicsPatchAck, Data: strconv.FormatUint(in.SurfaceRevision, 10)}); err != nil {
				return msg{}, err
			}
		}
	}
	return m, nil
}

func (c *endpointConnection) Close() error { return c.stream.Close() }
