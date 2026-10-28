// Package client attaches the local terminal to a running ikesu server:
// it puts the terminal in raw mode, forwards input and resizes over
// ikesu-client.sock, and renders the frames the server sends back.
package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

// attachTimeout is how long Attach waits for a freshly spawned server to
// bind its client socket.
const attachTimeout = 10 * time.Second

// drainMs mirrors the TUI's input drain before leaving the alt screen.
const drainMs = 100 * time.Millisecond

// msg is one JSON line on the client socket (see internal/server).
type msg struct {
	ClientView            *api.ClientView                      `json:"client_view,omitempty"`
	Terminal              *endpoint.Terminal                   `json:"terminal,omitempty"`
	GraphicsFileTransport string                               `json:"graphics_file_transport,omitempty"`
	GraphicsFile          *endpoint.GraphicsFile               `json:"graphics_file,omitempty"`
	GraphicsTransmission  *endpoint.GraphicsTransmission       `json:"graphics_transmission,omitempty"`
	GraphicsResult        *endpoint.GraphicsTransmissionResult `json:"graphics_result,omitempty"`
	snapshot              *render.Input
	traceBoot             string
	traceBytes            int
	traceTextOnly         bool
	InputSequence         uint64                 `json:"input_sequence,omitempty"`
	ProcessedInput        uint64                 `json:"processed_input,omitempty"`
	EndPrefix             bool                   `json:"end_prefix,omitempty"`
	Notification          *endpoint.Notification `json:"notification,omitempty"`
	endpoint.Peer
	Available     int                       `json:"available_generation,omitempty"`
	Type          string                    `json:"type"`
	Kind          string                    `json:"kind,omitempty"`
	Data          string                    `json:"data,omitempty"`
	SidebarJobs   *bool                     `json:"sidebar_jobs,omitempty"`
	Cols          int                       `json:"cols,omitempty"`
	Rows          int                       `json:"rows,omitempty"`
	Xpixel        int                       `json:"xpixel,omitempty"`
	Ypixel        int                       `json:"ypixel,omitempty"`
	Reason        string                    `json:"reason,omitempty"`
	Rendered      *render.Output            `json:"rendered,omitempty"`
	RequestAck    bool                      `json:"request_ack,omitempty"`
	FocusEpoch    uint64                    `json:"focus_epoch,omitempty"`
	Config        *config.Config            `json:"config,omitempty"`
	ConfigWarning string                    `json:"config_warning,omitempty"`
	Preferences   *endpoint.ViewPreferences `json:"preferences,omitempty"`
	ConfigEdit    *config.Edit              `json:"config_edit,omitempty"`
	ConfigID      uint64                    `json:"config_id,omitempty"`
	Visible       *bool                     `json:"visible,omitempty"`
	Workspace     int                       `json:"workspace,omitempty"`
	MachineAction string                    `json:"machine_action,omitempty"`
}

// Attach connects the local terminal to the session's server, spawning it
// if needed. It blocks until the client detaches or the server exits.
func Attach(session string, in *os.File, out *os.File, version string) (int, error) {
	if NestedBlocked() {
		return 1, errors.New("refusing to start a nested ikesu session (a ikesu server is an ancestor; set [experimental] allow_nested = true to permit, or use --no-session)")
	}
	settings, err := loadLocalSettings(session)
	if err != nil {
		return 1, err
	}
	settings.clientVersion = version
	if err := (machine.Catalog{Path: filepath.Join(api.SessionDir(session), "machines.json")}).Ensure(); err != nil {
		return 1, fmt.Errorf("machine catalog: %w", err)
	}

	dir := api.SessionDir(session)
	conn, err := dialClient(dir)
	if err != nil {
		if err := spawnServer(session, dir); err != nil {
			return 1, err
		}
		conn, err = waitDial(dir, attachTimeout)
		if err != nil {
			return 1, fmt.Errorf("could not reach the ikesu server for session %q: %w\nSee %s", session, err, api.LogPath(dir))
		}
	}

	// Terminal setup mirrors tui.Run.
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		conn.Close()
		return 1, fmt.Errorf("raw mode: %w", err)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = term.Restore(int(in.Fd()), old)
			restored = true
		}
	}
	left := false
	leave := func() {
		if left {
			return
		}
		left = true
		_, _ = out.WriteString(tui.MouseOffSeq)
		platform.FlushInput(int(in.Fd()))
		tui.DrainInput(in, drainMs)
		_, _ = out.WriteString(tui.KbdLeaveSeq() + tui.LeaveSeq)
		restore()
	}
	defer leave()
	defer restore()

	if _, err := out.WriteString(tui.EnterSeq + tui.KbdEnterSeq()); err != nil {
		conn.Close()
		return 1, err
	}
	result := pumpMachines(session, conn, in, out, settings, savedMachines(session))
	conn.Close()
	if result == pumpRejected {
		return 1, nil
	}
	return 0, nil
}

// pumpResult is how a single attach session ended.
type pumpResult int

const (
	pumpDone     pumpResult = iota // detached or server exit — client returns
	pumpRestart                    // server asked for a reconnect
	pumpClosed                     // conn dropped without a close message
	pumpRejected                   // explicit handshake or protocol refusal
)

// pump runs one attach session over conn: hello → input/resize forwarding
// → output rendering.
func pump(conn net.Conn, in, out *os.File, leave func(), settings *localSettings) pumpResult {
	ws, err := tui.GetWinsize(in)
	if err != nil {
		leave()
		fmt.Fprintf(out, "ikesu: winsize: %v\n", err)
		return pumpDone
	}
	w := &writer{conn: conn}
	if err := w.send(msg{Terminal: endpoint.LocalTerminal(), Peer: endpoint.Current(), Type: "hello", Cols: ws.Cols, Rows: ws.Rows, Xpixel: ws.Xpixel, Ypixel: ws.Ypixel, Config: settings.config, ConfigWarning: settings.warning, Preferences: settings.preferences}); err != nil {
		leave()
		fmt.Fprintf(out, "ikesu: attach: %v\n", err)
		return pumpDone
	}

	rd := bufio.NewReader(conn)
	line, err := rd.ReadBytes('\n')
	if err != nil {
		return pumpClosed
	}
	var welcome msg
	if err := json.Unmarshal(line, &welcome); err != nil {
		leave()
		fmt.Fprintln(out, "ikesu: invalid endpoint welcome:", err)
		return pumpRejected
	}
	if welcome.Type == "restart" {
		return pumpRestart
	}
	if welcome.Type == "exit" {
		return pumpDone
	}
	if welcome.Type == "error" {
		leave()
		fmt.Fprintln(out, "ikesu:", welcome.Reason)
		return pumpRejected
	}
	if welcome.Type != "welcome" {
		leave()
		fmt.Fprintln(out, "ikesu: endpoint_generation_unsupported: upgrade the server to endpoint generation 1 or later")
		return pumpRejected
	}
	available, err := endpoint.Negotiate(endpoint.Current(), welcome.Peer)
	if err != nil {
		leave()
		fmt.Fprintln(out, "ikesu:", err)
		return pumpRejected
	}
	w.available, w.methods = available, welcome.Methods

	// stdin → server input messages.
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if err := w.send(msg{Type: "input", Data: base64.StdEncoding.EncodeToString(buf[:n])}); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// SIGWINCH → resize messages.
	winch := make(chan os.Signal, 4)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	winchStop := make(chan struct{})
	defer close(winchStop)
	go func() {
		for {
			select {
			case <-winchStop:
				return
			case <-winch:
				if sz, err := tui.GetWinsize(in); err == nil {
					_ = w.send(msg{Type: "resize", Cols: sz.Cols, Rows: sz.Rows, Xpixel: sz.Xpixel, Ypixel: sz.Ypixel})
				}
			}
		}
	}()

	var snapshots endpoint.SnapshotDecoder
	var frames render.Encoder
	defer func() { _, _ = out.Write(frames.RestoreCursor()) }()
	windowTitle := windowTitleState{session: settings.session}
	var inputSource platform.InputSource
	defer inputSource.Prefix(false, false)
	var lastRegions *render.Output
	renderAckClosed := false
	// Server → terminal. The server supplies state and pane cells; only
	// this process composes sidebar, tabs, borders and overlays.
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			// Silent drop: Attach decides whether this is a restart
			// handoff (retry) or a real close (report).
			return pumpClosed
		}
		var m msg
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch m.Type {
		case "config_edit":
			if m.ConfigEdit == nil {
				return pumpRejected
			}
			cfg, err := settings.apply(*m.ConfigEdit)
			result := msg{Type: "config_result", ConfigID: m.ConfigID}
			if err != nil {
				result.Reason = err.Error()
			} else {
				settings.config = &cfg
				result.Config = &cfg
			}
			if err := w.send(result); err != nil {
				return pumpClosed
			}
		case "preferences":
			if m.Preferences == nil {
				continue
			}
			if err := settings.save(*m.Preferences); err != nil {
				if err := w.send(msg{Type: "preferences_error", Reason: "client preferences: " + err.Error()}); err != nil {
					return pumpClosed
				}
			}
		case "snapshot":
			data, err := base64.StdEncoding.DecodeString(m.Data)
			if err != nil {
				leave()
				fmt.Fprintln(out, "ikesu: invalid snapshot:", err)
				return pumpRejected
			}
			in, err := snapshots.Decode(data)
			if err != nil {
				leave()
				fmt.Fprintln(out, "ikesu: invalid snapshot:", err)
				return pumpRejected
			}
			if err := inputSource.Prefix(in.Prefix, in.SidebarConfig.Experimental.SwitchASCIIInputSourceInPrefix); err != nil {
				fmt.Fprintln(os.Stderr, "ikesu: input source:", err)
			}
			composed := render.ComposeClient(in, nil, settings.clientVersion, false)
			if in.SidebarConfig.UI.HostCursor == "drawn" && composed.AnchorVisible && composed.AnchorShape == 0 {
				if cell := composed.Frame.Cell(composed.AnchorX, composed.AnchorY); cell != nil {
					cell.Style.Attrs |= uv.AttrReverse
					composed.AnchorVisible = false
				}
			}
			data = frames.Frame(composed.Frame, composed.AnchorX, composed.AnchorY, composed.AnchorVisible, composed.AnchorShape)
			if lastRegions == nil {
				composed.Frame = nil
				if err := w.firstSnapshot(out, data, &windowTitle, in, composed, m.FocusEpoch); err != nil {
					if !isClosedRenderAck(err) {
						return pumpClosed
					}
					renderAckClosed = true
				}
				lastRegions = composed
				continue
			}
			_, _ = out.Write(data)
			if err := windowTitle.painted(out, in); err != nil {
				return pumpClosed
			}
			composed.Frame = nil
			if !renderAckClosed && (m.RequestAck || !reflect.DeepEqual(lastRegions, composed)) {
				if err := w.send(msg{Type: "rendered", Rendered: composed, FocusEpoch: m.FocusEpoch}); err != nil {
					if !isClosedRenderAck(err) {
						return pumpClosed
					}
					renderAckClosed = true
				}
				lastRegions = composed
			}
		case "welcome":
			if _, err := endpoint.Negotiate(endpoint.Current(), m.Peer); err != nil {
				leave()
				fmt.Fprintln(out, "ikesu:", err)
				return pumpDone
			}
		case "error":
			if strings.HasPrefix(m.Reason, "endpoint_action_unsupported:") {
				fmt.Fprintln(os.Stderr, "ikesu:", m.Reason)
				continue
			}
			leave()
			fmt.Fprintln(out, "ikesu:", m.Reason)
			return pumpDone
		case "notify":
			if m.Notification != nil && settings.config != nil {
				deliverDesktop(*m.Notification, *settings.config, settings.session, "")
			}
		case "output":
			if data, err := base64.StdEncoding.DecodeString(m.Data); err == nil {
				_, _ = out.Write(data)
			}
		case "detached":
			leave()
			if m.Reason != "" {
				fmt.Fprintf(out, "ikesu: detached: %s\n", m.Reason)
			} else {
				fmt.Fprintln(out, "ikesu: detached")
			}
			return pumpDone
		case "restart":
			return pumpRestart
		case "exit":
			return pumpDone
		}
	}
}

// writer serializes client→server messages.
type writer struct {
	conn      io.Writer
	mu        sync.Mutex
	available int
	methods   []string
}

func (w *writer) send(m msg) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sendLocked(m)
}

func (w *writer) sendLocked(m msg) error {
	if m.Type != "hello" && !endpoint.Allows(w.available, endpoint.RequiredGeneration(m.Type), w.methods, m.Type) {
		return fmt.Errorf("endpoint_action_unsupported: %s", m.Type)
	}
	err := json.NewEncoder(w.conn).Encode(m)
	if m.Type == "rendered" && errors.Is(err, syscall.EPIPE) {
		return &closedRenderAck{err}
	}
	return err
}

// The first visible frame must register its hit regions before stdin can
// overtake the acknowledgment. Later redraws keep accepting input normally.
func (w *writer) firstSnapshot(out *os.File, data []byte, title *windowTitleState, in render.Input, regions *render.Output, epoch uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := out.Write(data); err != nil {
		return err
	}
	if err := title.painted(out, in); err != nil {
		return err
	}
	return w.sendLocked(msg{Type: "rendered", Rendered: regions, FocusEpoch: epoch})
}

// dialClient tries the client socket once.
func dialClient(dir string) (net.Conn, error) {
	return net.Dial("unix", api.ClientSocketPath(dir))
}

// waitDial polls the client socket until the server is ready.
func waitDial(dir string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := dialClient(dir)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, lastErr
}

// spawnServer starts `ikesu server` detached, with its output going to the
// session's server.log. A NB flock on the spawn lock coordinates
// concurrent spawners: the lock holder spawns; others just poll for the
// socket. This lock is separate from the server's lifetime ikesu.lock so
// the child can take its lock before we release ours.
func spawnServer(session, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lf, err := os.OpenFile(api.SpawnLockPath(dir), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	if err := unix.Flock(int(lf.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		// Another process is spawning or already serving; the caller polls
		// the socket.
		lf.Close()
		return nil
	}
	defer func() {
		_ = unix.Flock(int(lf.Fd()), unix.LOCK_UN)
		lf.Close()
	}()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(api.LogPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open server log: %w", err)
	}
	defer logf.Close()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	// Global flags precede the subcommand so `server` arrives bare.
	cmd := childenv.Command(exe, "--session", session, "server")
	cmd.Args[0] = os.Args[0]
	cmd.Stdin = devNull
	cmd.Stdout = logf
	cmd.Stderr = logf
	// Inherit the client's cwd so the session's first pane opens there.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ikesu server: %w", err)
	}
	return cmd.Process.Release()
}
