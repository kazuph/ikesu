// Package client attaches the local terminal to a running gtx server:
// it puts the terminal in raw mode, forwards input and resizes over
// gtx-client.sock, and renders the frames the server sends back.
package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/tui"
)

// attachTimeout is how long Attach waits for a freshly spawned server to
// bind its client socket.
const attachTimeout = 10 * time.Second

// drainMs mirrors the TUI's input drain before leaving the alt screen.
const drainMs = 100 * time.Millisecond

// msg is one JSON line on the client socket (see internal/server).
type msg struct {
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Xpixel int    `json:"xpixel,omitempty"`
	Ypixel int    `json:"ypixel,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Attach connects the local terminal to the session's server, spawning it
// if needed. It blocks until the client detaches or the server exits.
func Attach(session string, in *os.File, out *os.File) (int, error) {
	if NestedBlocked() {
		return 1, errors.New("refusing to start a nested gtx session (a gtx server is an ancestor; set [experimental] allow_nested = true to permit, or use --no-session)")
	}

	dir := api.SessionDir(session)
	conn, err := dialClient(dir)
	if err != nil {
		if err := spawnServer(session, dir); err != nil {
			return 1, err
		}
		conn, err = waitDial(dir, attachTimeout)
		if err != nil {
			return 1, fmt.Errorf("could not reach the gtx server for session %q: %w\nSee %s", session, err, api.LogPath(dir))
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

	reconnecting := false
	var deadline time.Time
	for {
		res := pump(conn, in, out, leave)
		conn.Close()
		if res == pumpDone {
			return 0, nil
		}
		if res == pumpRestart {
			reconnecting = true
			deadline = time.Now().Add(attachTimeout)
		} else if !reconnecting {
			// pumpClosed without a restart notice: the server died.
			leave()
			fmt.Fprintln(out, "gtx: connection to the server closed")
			return 1, nil
		}
		// Reconnect to the successor. The terminal stays in alt-screen so
		// the handover is seamless. A conn handed out by the dying old
		// listener may close immediately; keep retrying until the
		// deadline (pumpClosed while reconnecting loops back here).
		conn = nil
		for conn == nil {
			if time.Now().After(deadline) {
				leave()
				fmt.Fprintln(out, "gtx: the server restarted but did not come back")
				return 1, nil
			}
			if c, err := dialClient(dir); err == nil {
				conn = c
			} else {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

// pumpResult is how a single attach session ended.
type pumpResult int

const (
	pumpDone    pumpResult = iota // detached or server exit — client returns
	pumpRestart                   // server asked for a reconnect
	pumpClosed                    // conn dropped without a close message
)

// pump runs one attach session over conn: hello → input/resize forwarding
// → output rendering.
func pump(conn net.Conn, in, out *os.File, leave func()) pumpResult {
	ws, err := tui.GetWinsize(in)
	if err != nil {
		leave()
		fmt.Fprintf(out, "gtx: winsize: %v\n", err)
		return pumpDone
	}
	w := &writer{conn: conn}
	if err := w.send(msg{Type: "hello", Cols: ws.Cols, Rows: ws.Rows, Xpixel: ws.Xpixel, Ypixel: ws.Ypixel}); err != nil {
		leave()
		fmt.Fprintf(out, "gtx: attach: %v\n", err)
		return pumpDone
	}

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

	// Server → terminal.
	rd := bufio.NewReader(conn)
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
		case "output":
			if data, err := base64.StdEncoding.DecodeString(m.Data); err == nil {
				_, _ = out.Write(data)
			}
		case "detached":
			leave()
			if m.Reason != "" {
				fmt.Fprintf(out, "gtx: detached: %s\n", m.Reason)
			} else {
				fmt.Fprintln(out, "gtx: detached")
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
	conn net.Conn
	mu   sync.Mutex
}

func (w *writer) send(m msg) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return json.NewEncoder(w.conn).Encode(m)
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

// spawnServer starts `gtx server` detached, with its output going to the
// session's server.log. A NB flock on the spawn lock coordinates
// concurrent spawners: the lock holder spawns; others just poll for the
// socket. This lock is separate from the server's lifetime gtx.lock so
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
	cmd := exec.Command(exe, "--session", session, "server")
	cmd.Stdin = devNull
	cmd.Stdout = logf
	cmd.Stderr = logf
	// Inherit the client's cwd so the session's first pane opens there.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gtx server: %w", err)
	}
	return cmd.Process.Release()
}
