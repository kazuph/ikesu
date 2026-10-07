// Package server runs the gtx background server: it owns the PTY
// supervisor, the model/application state, rendering, the API socket
// (gtx.sock), and the client attach socket (gtx-client.sock). The server
// outlives clients so panes keep running across attach/detach.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/tui"
)

// clientMsg is one JSON line on gtx-client.sock in either direction.
//
// Server→client: {"type":"output","data":"<base64 ANSI>"}|
//
//	{"type":"detached","reason":"..."}|{"type":"exit"}
//
// Client→server: {"type":"hello","cols":N,"rows":M}|
//
//	{"type":"input","data":"<base64>"}|{"type":"resize","cols":N,"rows":M}
type clientMsg struct {
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Server is one running session.
type Server struct {
	session string
	dir     string
	version string
	cwd     string // cwd the session was started from

	lock     *os.File
	apiLn    net.Listener
	clientLn net.Listener

	app *tui.App
	sup *runtime.Supervisor

	mu     sync.Mutex
	client *clientConn // currently attached display client, or nil

	apiWG sync.WaitGroup // in-flight API connections (drained on shutdown)
}

// clientWriteTimeout bounds how long a frame write to the attached client
// may block. A client that stops reading (suspended over ssh, Ctrl-Z)
// must not stall the app loop — and with it the API — so after the
// deadline the connection is dropped.
var clientWriteTimeout = 5 * time.Second

// clientConn is one attached display client.
type clientConn struct {
	conn net.Conn
	wmu  sync.Mutex
}

func (c *clientConn) send(m clientMsg) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
	defer c.conn.SetWriteDeadline(time.Time{})
	enc := json.NewEncoder(c.conn)
	return enc.Encode(m)
}

// Run starts the server for session and blocks until the app quits.
// version is the build version reported by ping.
func Run(session, version string) (int, error) {
	if !api.ValidSessionName(session) {
		return 2, fmt.Errorf("invalid session name %q", session)
	}
	dir := api.SessionDir(session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 1, err
	}

	// One server per session: flock the lock file for the server's lifetime.
	lockPath := api.LockPath(dir)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 1, fmt.Errorf("open lock: %w", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return 1, fmt.Errorf("a gtx server is already running for session %q", session)
	}

	apiSock := api.APISocketPath(dir)
	clientSock := api.ClientSocketPath(dir)
	// Stale sockets from a crashed server are safe to replace while we hold
	// the lock.
	os.Remove(apiSock)
	os.Remove(clientSock)
	apiLn, err := net.Listen("unix", apiSock)
	if err != nil {
		lock.Close()
		return 1, fmt.Errorf("listen %s: %w", apiSock, err)
	}
	clientLn, err := net.Listen("unix", clientSock)
	if err != nil {
		apiLn.Close()
		os.Remove(apiSock)
		lock.Close()
		return 1, fmt.Errorf("listen %s: %w", clientSock, err)
	}
	os.Chmod(apiSock, 0o600)
	os.Chmod(clientSock, 0o600)

	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.Getenv("HOME")
	}

	s := &Server{session: session, dir: dir, version: version, cwd: cwd, lock: lock, apiLn: apiLn, clientLn: clientLn}

	sup := runtime.NewSupervisor()
	apiSockAbs := apiSock
	sup.Env = func(paneID int) []string {
		return []string{
			fmt.Sprintf("GTX_PANE_ID=p%d", paneID),
			"GTX_SOCKET_PATH=" + apiSockAbs,
		}
	}
	s.sup = sup

	st := model.New(cwd, 120, 40)
	s.app = tui.NewApp(st, sup, &clientSink{s: s}, cwd)
	s.app.SetDetacher(func(reason string) { s.detach(reason) })
	s.app.SetSinkGate(func() bool { return s.hasClient() })
	s.app.SetQuitHook(func() { s.notifyExit() })

	if err := s.app.StartInitialPane(); err != nil {
		s.cleanup()
		return 1, fmt.Errorf("spawn pane: %w", err)
	}

	appDone := make(chan struct{})
	go func() {
		s.app.Run()
		close(appDone)
	}()
	go s.apiLoop()
	go s.clientLoop()

	<-appDone
	s.cleanup()
	return 0, nil
}

func (s *Server) cleanup() {
	// notifyExit already told the attached client; drop whatever remains.
	s.detach("server exiting")
	_ = s.apiLn.Close()
	_ = s.clientLn.Close()
	// Let in-flight API responses flush before we exit.
	drained := make(chan struct{})
	go func() { s.apiWG.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(500 * time.Millisecond):
	}
	s.sup.KillAll()
	os.Remove(api.APISocketPath(s.dir))
	os.Remove(api.ClientSocketPath(s.dir))
	_ = unix.Flock(int(s.lock.Fd()), unix.LOCK_UN)
	s.lock.Close()
}

// hasClient reports whether a display client is attached.
func (s *Server) hasClient() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil
}

// detach drops the attached client after telling it why.
func (s *Server) detach(reason string) {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		_ = c.send(clientMsg{Type: "detached", Reason: reason})
		_ = c.conn.Close()
	}
}

// notifyExit tells the attached client the session ended, then drops it.
func (s *Server) notifyExit() {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		_ = c.send(clientMsg{Type: "exit"})
		_ = c.conn.Close()
	}
}

// clientSink is the App's io.Writer: encoded frames go to the attached
// client as output messages; with no client they are dropped.
type clientSink struct {
	s *Server
}

func (w *clientSink) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	c := w.s.client
	w.s.mu.Unlock()
	if c == nil {
		return len(p), nil
	}
	if err := c.send(clientMsg{Type: "output", Data: encode64(p)}); err != nil {
		w.s.dropConn(c)
	}
	return len(p), nil
}

// dropConn removes c if it is still the attached client (broken pipe).
func (s *Server) dropConn(c *clientConn) {
	s.mu.Lock()
	if s.client == c {
		s.client = nil
	}
	s.mu.Unlock()
	_ = c.conn.Close()
}

// clientLoop accepts display clients until the listener closes.
func (s *Server) clientLoop() {
	for {
		conn, err := s.clientLn.Accept()
		if err != nil {
			return
		}
		go s.attach(conn)
	}
}

// attach registers conn as the display client, displacing any previous
// one, then pumps its input/resize messages into the app.
func (s *Server) attach(conn net.Conn) {
	c := &clientConn{conn: conn}

	// First message must be hello carrying the client's size.
	rd := bufio.NewReader(conn)
	hello, err := readClientMsg(rd)
	if err != nil || hello.Type != "hello" {
		conn.Close()
		return
	}

	s.mu.Lock()
	old := s.client
	s.client = c
	s.mu.Unlock()
	if old != nil {
		_ = old.send(clientMsg{Type: "detached", Reason: "another client attached"})
		_ = old.conn.Close()
	}

	// New client: force a full repaint at its size.
	s.app.Do(func() {
		s.app.ResetEncoder()
		s.app.ExecCmd(model.CmdResize{Cols: hello.Cols, Rows: hello.Rows})
	})

	for {
		m, err := readClientMsg(rd)
		if err != nil {
			s.dropConn(c)
			return
		}
		switch m.Type {
		case "input":
			if data, err := decode64(m.Data); err == nil && len(data) > 0 {
				s.app.PushInput(data)
			}
		case "resize":
			s.app.Resize(m.Cols, m.Rows)
		}
	}
}
