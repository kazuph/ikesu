// Package server runs the gtx background server: it owns the PTY
// supervisor, the model/application state, rendering, the API socket
// (gtx.sock), and the client attach socket (gtx-client.sock). The server
// outlives clients so panes keep running across attach/detach.
package server

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/plugin"
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
	Xpixel int    `json:"xpixel,omitempty"`
	Ypixel int    `json:"ypixel,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Server is one running session.
type Server struct {
	plugins           *plugin.Engine
	pluginEvents      bool
	pluginPanes       map[int]pluginPaneOwner
	agentLaunches     map[int]*agentLaunch
	closing           atomic.Uint32 // 1: exit, 2: restart; prevents a reconnect entering the retiring server
	workspaceMetadata map[int]map[string]*workspaceMetadata
	paneMetadata      map[int]*paneMetadata
	reportSources     map[int]string
	session           string
	dir               string
	version           string
	cwd               string // cwd the session was started from

	lock     *os.File
	apiLn    net.Listener
	clientLn net.Listener

	app *tui.App
	sup *runtime.Supervisor

	mu     sync.Mutex
	client *clientConn // currently attached display client, or nil

	apiWG sync.WaitGroup // in-flight API connections (drained on shutdown)

	// durable mailbox / jobs
	dbOnce sync.Once
	store  *dispatch.Store
	dbErr  error
	mailCh chan mailWrite

	mailMu       sync.Mutex
	mailInFlight map[int64]bool  // regular message ids staged for PTY write
	jobInFlight  map[string]bool // job-mail batch keys staged for PTY write

	jobProcs jobProcsT // live background jobs (external job id → process)
	jobCache jobCache

	// notifications (SPEC G4)
	cfgError      string
	cfg           config.Config
	lastStatus    map[int]string               // last seen status key per pane
	notifyLast    map[int]map[string]time.Time // pane → kind → last shown
	lastAttention time.Time                    // last needs_attention anywhere

	// session persistence & restart (SPEC G3/G5)
	saveMu     sync.Mutex
	saveTimer  *time.Timer // pending coalesced session.json write
	restarting bool        // set on the app loop; cleanup respawns a server

	// observedStart remembers the process start an observed session id
	// was recorded for, so the same process is never re-observed.
	observedStart map[int]time.Time

	// domain event bus (SPEC G1 events): journal replay + live push
	evMu      sync.Mutex
	journal   []api.EventEnvelope
	subs      map[*eventSub]bool
	waiters   []*eventWaiter
	outEmit   map[int]*outputEmit // pane output emit throttle
	lastAgent map[int]string      // last detected agent id per pane
}

// clientWriteTimeout bounds how long a frame write to the attached client
// may block. A client that stops reading (suspended over ssh, Ctrl-Z)
// must not stall the app loop — and with it the API — so after the
// deadline the connection is dropped.
var clientWriteTimeout = 5 * time.Second

// clientConn is one attached display client.
type clientConn struct {
	conn    net.Conn
	wmu     sync.Mutex
	id      int
	once    sync.Once
	render  chan clientWrite
	control chan clientWrite
	done    chan struct{}
	closed  bool
}

var clientSequence atomic.Int64

func (c *clientConn) startWriter() {
	c.once.Do(func() {
		if c.id == 0 {
			c.id = int(clientSequence.Add(1))
		}
		c.render = make(chan clientWrite)
		c.control = make(chan clientWrite)
		c.done = make(chan struct{})
		go runClientWriter(c.conn, c.id, c.render, c.control, c.done)
	})
}

func (c *clientConn) send(m clientMsg) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	c.startWriter()
	request := clientWrite{message: m, reply: make(chan clientWriteResult, 1)}
	queue := c.control
	if m.Type == "output" {
		queue = c.render
	}
	select {
	case queue <- request:
	case <-c.done:
		return net.ErrClosed
	}
	select {
	case result := <-request.reply:
		return result.err
	case <-c.done:
		select {
		case result := <-request.reply:
			return result.err
		default:
			return net.ErrClosed
		}
	}
}

func (c *clientConn) close() {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.startWriter()
	close(c.control)
	<-c.done
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

	s := &Server{session: session, dir: dir, version: version, cwd: cwd, lock: lock, apiLn: apiLn, clientLn: clientLn,
		mailCh: make(chan mailWrite, 256), mailInFlight: map[int64]bool{}, jobInFlight: map[string]bool{},
		jobProcs:   jobProcsT{m: map[string]*exec.Cmd{}},
		lastStatus: map[int]string{}, notifyLast: map[int]map[string]time.Time{}, observedStart: map[int]time.Time{}}

	if cfg, err := config.Load(); err == nil {
		s.cfg = cfg
	} else {
		s.cfgError = err.Error()
		fmt.Fprintf(os.Stderr, "gtx: %s: %v\n", config.Path(), err)
	}

	sup := runtime.NewSupervisor()
	sup.StartLoginPath()
	apiSockAbs := apiSock
	sup.Env = func(paneID int) []string {
		env := []string{
			fmt.Sprintf("GTX_PANE_ID=p%d", paneID),
			"GTX_SOCKET_PATH=" + apiSockAbs,
			"GTX_SOCKET_PATH_EXPLICIT=1",
		}
		// Spawn runs on the app loop, so State() is safe here and the pane
		// already exists in the model when its shell starts.
		if st := s.app.State(); st != nil {
			if wsIdx, tabIdx := st.FindPane(paneID); wsIdx >= 0 {
				ws := st.Workspaces[wsIdx]
				env = append(env,
					"GTX_WORKSPACE_ID="+fmtWorkspaceID(ws.ID),
					"GTX_TAB_ID="+fmtTabID(ws.ID, ws.Tabs[tabIdx].ID),
				)
			}
		}
		return env
	}
	s.sup = sup

	st := model.New(cwd, 120, 40)
	s.app = tui.NewApp(st, sup, &clientSink{s: s}, cwd)
	if s.cfgError != "" {
		s.app.SetConfigWarning(s.cfgError)
	}
	s.app.SetDetacher(func(reason string) { s.detach(reason) })
	s.app.SetSinkGate(func() bool { return s.hasClient() })
	s.app.SetQuitHook(func() {
		if s.restarting {
			s.closing.Store(2)
		} else {
			s.closing.Store(1)
		}
		s.saveSession()
		if s.restarting {
			s.notifyRestart()
		} else {
			s.notifyExit()
		}
	})
	s.app.SetChangeHook(s.markSessionDirty)
	s.app.SetRestartHook(s.requestRestart)
	s.app.SetRestoreHook(func(dryRun bool) { s.runAgentRestore(dryRun) })
	s.app.SetPaneClosedHook(func(pid int) { s.ledgerDel(pid); delete(s.pluginPanes, pid) })
	s.app.SetWarningsHook(s.agentSessionWarnings)
	s.jobCache.wake = make(chan struct{}, 1)
	s.app.SetJobSource(s.cachedJobSnapshot)
	s.app.SetWorkspaceTokenSource(s.workspaceTokens)
	s.app.SetConfigChanged(func(c config.Config) { s.cfg = c })
	s.app.SetPaneMetadataSource(s.paneMetadataView)
	// Mail delivery and agent notifications fire on status transitions
	// (and once at startup below) — event-driven, never polled.
	s.app.SetAvailHook(func(pid int) {
		if s.agentLaunches[pid] != nil {
			if wi, ti := s.app.State().FindPane(pid); wi >= 0 {
				s.agentInfoAt(pid, wi, ti)
			}
		}
		s.onPaneStatus(pid)
		s.emitAgentEvents(pid)
	})
	// Domain events (SPEC G1): command diffs, exits, and output changes.
	s.outEmit = map[int]*outputEmit{}
	s.lastAgent = map[int]string{}
	s.initPlugins()
	s.app.SetPostExecHook(s.afterExec)
	s.app.SetWorktreeHook(s.worktreeEvent)
	s.app.SetExitedHook(s.onPaneExited)
	s.app.SetOutputHook(s.onPaneOutput)
	if r, ok := config.ParseNavKey(s.cfg.NavKey()); ok {
		s.app.SetNavKey(r)
	} else {
		s.app.SetNavKey('o')
	}
	s.app.SetUIFlags(s.cfg.ShowTabBar(), s.cfg.HideTabBarWhenSingleTab(), s.cfg.PaneBorders())
	s.app.SetSidebarConfig(s.cfg, true)
	s.app.SetKeybinds(s.cfg.CopyKeys(), s.cfg.SplitVerticalKeys(), s.cfg.SplitHorizontalKeys())
	s.app.SetPaneKeybinds(s.cfg)

	// Restore the saved session when session.json exists; otherwise start
	// the initial shell. A corrupt file fails closed to a fresh state.
	if f := s.loadSession(); f != nil {
		s.app.RestoreSession(f)
		s.scheduleAutoResume()
	} else if err := s.app.StartInitialPane(); err != nil {
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
	go s.mailWorker()
	// These startup sweeps all open the SQLite store; when the DB file has
	// never been created there is nothing to seed, abort, or flush, so skip
	// them to keep the engine (and its memory) out of the process.
	if dispatch.DBExists() {
		s.seedJobSeq()
		s.abortOrphanedJobs()
		s.app.Do(func() { s.flushAllMail() })
	}

	jobsDone := make(chan struct{})
	go s.jobCacheLoop(appDone, jobsDone)
	<-appDone
	<-jobsDone
	s.cleanup()
	if s.restarting {
		s.spawnSuccessor()
	}
	return 0, nil
}

func (s *Server) cleanup() {
	s.saveMu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.saveMu.Unlock()
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
	close(s.mailCh)
	s.sup.KillAll()
	if s.store != nil {
		s.store.Close()
	}
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
		c.close()
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
		c.close()
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
		return 0, err
	}
	return len(p), nil
}

// writeClientOutput injects raw bytes into the attached client's output
// stream (used for terminal-delivery notifications).
func (s *Server) writeClientOutput(data string) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return
	}
	if err := c.send(clientMsg{Type: "output", Data: encode64([]byte(data))}); err != nil {
		s.dropConn(c)
	}
}

// dropConn removes c if it is still the attached client (broken pipe).
func (s *Server) dropConn(c *clientConn) {
	s.mu.Lock()
	if s.client == c {
		s.client = nil
	}
	s.mu.Unlock()
	c.close()
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
	if closing := s.closing.Load(); closing != 0 {
		s.mu.Unlock()
		kind := "exit"
		if closing == 2 {
			kind = "restart"
		}
		_ = c.send(clientMsg{Type: kind})
		c.close()
		return
	}
	old := s.client
	s.client = c
	s.mu.Unlock()
	if old != nil {
		_ = old.send(clientMsg{Type: "detached", Reason: "another client attached"})
		old.close()
	}

	// New client: force a full repaint at its size.
	s.app.Do(func() {
		s.app.ResetEncoder()
		s.app.ExecCmd(model.CmdResize{Cols: hello.Cols, Rows: hello.Rows})
		s.app.BeginHostMouse(hello.Xpixel, hello.Ypixel)
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
			s.app.Resize(m.Cols, m.Rows, m.Xpixel, m.Ypixel)
		}
	}
}
