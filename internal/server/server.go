// Package server runs the ikesu background server: it owns the PTY
// supervisor, the model/application state, rendering, the API socket
// (ikesu.sock), and the client attach socket (ikesu-client.sock). The server
// outlives clients so panes keep running across attach/detach.
package server

import (
	"bufio"
	"fmt"
	"github.com/kazuph/ikesu/internal/benchstage"
	"github.com/kazuph/ikesu/internal/childenv"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/latency"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/plugin"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/tui"
)

// clientMsg is one JSON line on ikesu-client.sock in either direction.
//
// Server→client: {"type":"output","data":"<base64 ANSI>"}|
//
//	{"type":"detached","reason":"..."}|{"type":"exit"}
//
// Client→server: {"type":"hello","cols":N,"rows":M}|
//
//	{"type":"input","data":"<base64>"}|{"type":"resize","cols":N,"rows":M}
type clientMsg struct {
	Terminal              *endpoint.Terminal                   `json:"terminal,omitempty"`
	GraphicsFileTransport string                               `json:"graphics_file_transport,omitempty"`
	GraphicsFile          *endpoint.GraphicsFile               `json:"graphics_file,omitempty"`
	GraphicsTransmission  *endpoint.GraphicsTransmission       `json:"graphics_transmission,omitempty"`
	GraphicsResult        *endpoint.GraphicsTransmissionResult `json:"graphics_result,omitempty"`
	EndPrefix             bool                                 `json:"end_prefix,omitempty"`
	Notification          *endpoint.Notification               `json:"notification,omitempty"`
	endpoint.Peer
	Available      int                          `json:"available_generation,omitempty"`
	Type           string                       `json:"type"`
	Kind           string                       `json:"kind,omitempty"`
	Data           string                       `json:"data,omitempty"`
	SidebarJobs    *bool                        `json:"sidebar_jobs,omitempty"`
	Cols           int                          `json:"cols,omitempty"`
	Rows           int                          `json:"rows,omitempty"`
	Xpixel         int                          `json:"xpixel,omitempty"`
	Ypixel         int                          `json:"ypixel,omitempty"`
	Reason         string                       `json:"reason,omitempty"`
	Rendered       *render.Output               `json:"rendered,omitempty"`
	RequestAck     bool                         `json:"request_ack,omitempty"`
	FocusEpoch     uint64                       `json:"focus_epoch,omitempty"`
	Config         *config.Config               `json:"config,omitempty"`
	ConfigWarning  string                       `json:"config_warning,omitempty"`
	Preferences    *endpoint.ViewPreferences    `json:"preferences,omitempty"`
	ConfigEdit     *config.Edit                 `json:"config_edit,omitempty"`
	ConfigID       uint64                       `json:"config_id,omitempty"`
	Visible        *bool                        `json:"visible,omitempty"`
	Workspace      int                          `json:"workspace,omitempty"`
	Decisions      *endpoint.DecisionProjection `json:"decisions,omitempty"`
	DecisionAnswer *api.DecisionAnswerParams    `json:"decision_answer,omitempty"`
	MachineAction  string                       `json:"machine_action,omitempty"`
}

// Server is one running session.
type Server struct {
	trace               *latency.Trace
	graphicsFiles       graphicsFileHub
	graphicsStreamOwner atomic.Uint64
	handoffMu           sync.Mutex
	handedOff           bool
	decisions           decisionHub
	screenPrompts       screenPromptHub
	plugins             *plugin.Engine
	pluginEvents        bool
	pluginPanes         map[int]pluginPaneOwner
	agentLaunches       map[int]*agentLaunch
	closing             atomic.Uint32 // 1: exit, 2: restart; prevents a reconnect entering the retiring server
	workspaceMetadata   map[int]map[string]*workspaceMetadata
	paneMetadata        map[int]*paneMetadata
	reportSources       map[int]string
	session             string
	dir                 string
	version             string
	endpointPeer        endpoint.Peer
	cwd                 string // cwd the session was started from
	configPath          string // absolute path read when this server started

	lock     *os.File
	apiLn    net.Listener
	clientLn net.Listener

	app *tui.App
	sup *runtime.Supervisor

	mu               sync.Mutex
	client           *clientConn // last-interacted client, used for targeted terminal notifications
	agentAttachWG    sync.WaitGroup
	agentControllers map[int]*clientConn
	clients          map[*clientConn]bool
	lastUse          uint64

	apiWG sync.WaitGroup // in-flight API connections (drained on shutdown)

	// durable mailbox / jobs
	dbOnce sync.Once
	store  *dispatch.Store
	dbErr  error
	mailCh chan mailWrite
	mailWG sync.WaitGroup

	mailMu          sync.Mutex
	mailDelivery    sync.RWMutex    // fence PTY write + acknowledgement before handoff
	mailAwaitingAck []mailWrite     // written to PTY; retry only these exact IDs
	mailInFlight    map[int64]bool  // regular message ids staged for PTY write
	jobInFlight     map[string]bool // job-mail batch keys staged for PTY write

	jobProcs jobProcsT // local job numbering and cancellation monitors
	jobCache jobCache

	// notifications (SPEC G4)
	cfgError            string
	cfg                 config.Config
	lastStatus          map[int]string               // last seen status key per pane
	notifyLast          map[int]map[string]time.Time // pane → kind → last shown
	lastAttention       time.Time                    // last needs_attention anywhere
	lastAPINotification time.Time                    // last successfully shown explicit notification

	// session persistence & restart (SPEC G3/G5)
	saveMu        sync.Mutex
	saveTimer     *time.Timer // pending coalesced session.json write
	saveWriteMu   sync.Mutex
	saveSequence  uint64 // assigned only on the app loop
	savedSequence uint64 // guarded by saveWriteMu
	saveClosed    bool   // guarded by saveWriteMu
	restarting    bool   // set on the app loop; cleanup respawns a server

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
	terminal       *endpoint.Terminal
	directGraphics bool
	directDisabled bool
	graphics       *graphicsFlow
	configEditor   func(config.Edit, func(config.Config, error))
	viewer         *tui.Viewer
	available      int
	conn           net.Conn
	wmu            sync.Mutex
	id             int
	lastUse        uint64
	once           sync.Once
	render         chan clientWrite
	control        chan clientWrite
	done           chan struct{}
	closed         bool
}

var clientSequence atomic.Int64

func (c *clientConn) startWriter() {
	c.once.Do(func() {
		if c.id == 0 {
			c.id = int(clientSequence.Add(1))
		}
		c.render = make(chan clientWrite, 1)
		c.control = make(chan clientWrite, 1)
		c.done = make(chan struct{})
		go runClientWriter(c.conn, c.id, c.render, c.control, c.done)
	})
}

func (c *clientConn) send(m clientMsg) error {
	defer benchstage.Span("server.send")()
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
	return runEndpoint(session, version, endpoint.GraphicsPeer())
}

func runEndpoint(session, version string, peer endpoint.Peer, imports ...*handoffImport) (int, error) {
	childenv.ScrubProcess()
	var imported *handoffImport
	if len(imports) > 0 {
		imported = imports[0]
	}
	if !api.ValidSessionName(session) {
		return 2, fmt.Errorf("invalid session name %q", session)
	}
	configPath, err := filepath.Abs(config.Path())
	if err != nil {
		return 1, fmt.Errorf("resolve configuration path: %w", err)
	}
	dir := api.SessionDir(session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 1, err
	}

	var lock *os.File
	var apiLn, clientLn net.Listener
	if imported != nil {
		unix.CloseOnExec(6)
		lock = os.NewFile(6, "handoff-lock")
		apiLn, err = inheritedListener(4)
		if err == nil {
			clientLn, err = inheritedListener(5)
		}
	} else {
		lock, apiLn, clientLn, err = openSessionSockets(session, dir)
	}
	if err != nil {
		return 1, err
	}
	// A live importer owns the inherited listener, whose pathname can belong
	// to a previous executable namespace. That is the endpoint actually listening.
	apiSock := apiLn.Addr().String()

	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.Getenv("HOME")
	}

	s := &Server{session: session, dir: dir, version: version, endpointPeer: peer, cwd: cwd, lock: lock, apiLn: apiLn, clientLn: clientLn,
		mailCh: make(chan mailWrite, 256), mailInFlight: map[int64]bool{}, jobInFlight: map[string]bool{},
		lastStatus: map[int]string{}, notifyLast: map[int]map[string]time.Time{}, observedStart: map[int]time.Time{}}
	s.configPath = configPath
	if latency.Enabled {
		s.trace = latency.Open(api.LogPath(dir))
	}

	if cfg, err := config.Load(); err == nil {
		s.cfg = cfg
		for _, warning := range cfg.Warnings {
			if strings.HasPrefix(warning, "invalid keybinding:") || strings.HasPrefix(warning, "keys.") {
				fmt.Fprintf(os.Stderr, "ikesu: config diagnostic %s\n", warning)
			}
		}
	} else {
		s.cfgError = err.Error()
		fmt.Fprintf(os.Stderr, "ikesu: %s: %v\n", config.Path(), err)
	}

	sup := runtime.NewSupervisor()
	sup.SetGraphicsVersion(version)
	sup.StartLoginPath()
	apiSockAbs := apiSock
	sup.Env = func(paneID int) []string {
		env := []string{
			fmt.Sprintf("IKESU_PANE_ID=p%d", paneID),
			"IKESU_SOCKET_PATH=" + apiSockAbs,
			"IKESU_SOCKET_PATH_EXPLICIT=1",
			"IKESU_CONFIG_PATH=" + s.configPath,
		}
		// Spawn runs on the app loop, so State() is safe here and the pane
		// already exists in the model when its shell starts.
		if st := s.app.State(); st != nil {
			if wsIdx, tabIdx := st.FindPane(paneID); wsIdx >= 0 {
				ws := st.Workspaces[wsIdx]
				env = append(env,
					"IKESU_WORKSPACE_ID="+fmtWorkspaceID(ws.ID),
					"IKESU_TAB_ID="+fmtTabID(ws.ID, ws.Tabs[tabIdx].ID),
				)
			}
		}
		return env
	}
	s.sup = sup

	st := model.New(cwd, 120, 40)
	s.app = tui.NewApp(st, sup, &clientSink{s: s}, cwd)
	s.app.SetServerUI()
	s.app.SetMachineCatalog(filepath.Join(api.SessionDir(s.session), "machines.json"))
	if s.cfgError != "" {
		s.app.SetConfigWarning(s.cfgError)
	}
	s.app.SetDetacher(func(reason string) { s.detach(reason) })
	s.app.SetSinkGate(func() bool { return s.hasClient() })
	s.app.SetViewerHooks(s.drawViewers, s.tickViewers, s.visiblePane)
	s.app.SetQuitHook(func() {
		s.stopDecisions()
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
	s.app.SetConfigChanged(s.screenPromptConfig)
	s.app.SetPaneMetadataSource(s.paneMetadataView)
	s.app.UsePaneMetadataEvents(s.paneMetadataNextExpiry)
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
		s.scheduleScreenPromptPane(pid)
	})
	// Domain events (SPEC G1): command diffs, exits, and output changes.
	s.outEmit = map[int]*outputEmit{}
	s.lastAgent = map[int]string{}
	s.initPlugins()
	s.app.SetPostExecHook(s.afterExec)
	s.app.SetWorktreeHook(s.worktreeEvent)
	s.app.SetExitedHook(s.onPaneExited)
	s.app.SetOutputHook(s.onPaneOutput)
	s.app.SetClipboardHook(s.deliverPaneClipboard)
	if r, ok := config.ParseNavKey(s.cfg.NavKey()); ok {
		s.app.SetNavKey(r)
	} else {
		s.app.SetNavKey('o')
	}
	s.app.SetUIFlags(s.cfg.ShowTabBar(), s.cfg.HideTabBarWhenSingleTab(), s.cfg.PaneBorders())
	s.app.SetSidebarConfig(s.cfg, true)
	s.app.SetPluginHost(s.ensurePlugins, func() plugin.Context { return s.pluginContext("", nil) })
	s.app.SetKeybinds(s.cfg.CopyKeys(), s.cfg.SplitVerticalKeys(), s.cfg.SplitHorizontalKeys())
	s.app.SetPaneKeybinds(s.cfg)

	// Restore the saved session when session.json exists; otherwise start
	// the initial shell. A corrupt file fails closed to a fresh state.
	if imported != nil {
		if err := s.restoreLive(imported, apiSock); err != nil {
			s.handedOff = true
			s.sup.ReleaseHandoff()
			s.cleanup()
			return 1, err
		}
	} else if f := s.loadSession(); f != nil {
		s.app.RestoreSession(f)
		s.restorePaneHistory(f)
		s.scheduleAutoResume()
	} else if err := s.app.StartInitialPane(); err != nil {
		s.cleanup()
		return 1, fmt.Errorf("spawn pane: %w", err)
	}

	if imported == nil {
		s.app.StartOnboarding()
	}
	appDone := make(chan struct{})
	go func() {
		s.app.Run()
		close(appDone)
	}()
	if imported != nil {
		if err := imported.Encoder.Encode(handoffResult{State: "running"}); err != nil {
			s.handedOff = true
			s.sup.ReleaseHandoff()
			s.app.Do(s.app.RetireHandoff)
		}
		_ = imported.Control.SetDeadline(time.Time{})
	}
	s.mailWG.Add(1)
	go func() { defer s.mailWG.Done(); s.mailWorker() }()
	// These startup sweeps all open the SQLite store; when the DB file has
	// never been created there is nothing to seed, abort, or flush, so skip
	// them to keep the engine (and its memory) out of the process.
	if dispatch.DBExists(s.session) {
		s.seedJobSeq()
		if imported == nil {
			s.abortOrphanedJobs()
		}
		s.app.Do(func() {
			s.flushAllMail()
			s.initDecisionHub()
			if store, err := s.db(); err == nil {
				s.syncDecisionAttention()
				if err := s.expireDecisions(store); err != nil {
					s.app.SetConfigWarning(err.Error())
				}
			}
		})
	}

	go s.apiLoop()
	go s.clientLoop()
	jobsDone := make(chan struct{})
	s.app.Do(func() { s.screenPromptConfig(s.cfg) })
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
	defer s.trace.Close()
	s.saveMu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.saveMu.Unlock()
	s.closeSessionWrites()
	// notifyExit already told the attached client; drop whatever remains.
	s.detach("server exiting")
	s.closeAgentAttachments()
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
	if !s.handedOff {
		s.sup.KillAll()
	}
	s.mailWG.Wait()
	s.stopJobMonitors()
	if s.store != nil {
		s.store.Close()
	}
	if !s.handedOff {
		os.Remove(api.APISocketPath(s.dir))
		os.Remove(api.ClientSocketPath(s.dir))
		_ = unix.Flock(int(s.lock.Fd()), unix.LOCK_UN)
	}
	s.lock.Close()
	if s.graphicsFiles.source != nil {
		s.graphicsFiles.source.close()
	}
}

// hasClient reports whether a display client is attached.
func (s *Server) hasClient() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients) > 0 || s.client != nil
}

// detach drops the currently interacting display, leaving other viewers attached.
func (s *Server) detach(reason string) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		s.detachClient(c, reason)
	}
}

func (s *Server) detachClient(c *clientConn, reason string) {
	_ = c.send(clientMsg{Type: "detached", Reason: reason})
	s.dropConn(c)
}

func (s *Server) closeClients(kind string) {
	for _, c := range s.displayClients() {
		_ = c.send(clientMsg{Type: kind})
		s.dropConn(c)
	}
}

func (s *Server) notifyExit() { s.closeClients("exit") }

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
	wasAttached := s.clients[c]
	delete(s.clients, c)
	if s.client == c {
		s.client = nil
		for other := range s.clients {
			s.client = other
			break
		}
	}
	s.mu.Unlock()
	c.close()
	if wasAttached && s.app != nil {
		go s.releaseSizeViewer(c)
	}
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

// attach negotiates display capabilities before adding an independent viewer.
func (s *Server) attach(conn net.Conn) {
	c := &clientConn{conn: conn}
	if s.trace != nil {
		c.id = int(clientSequence.Add(1))
		if peer := latency.PeerPID(conn); peer > 0 {
			s.trace.Event("client.accepted", " client_id=", c.id, " peer_pid=", peer)
		}
	}
	rd := bufio.NewReader(conn)
	hello, err := readClientMsg(rd)
	if err == nil && hello.Type == "agent_attach" {
		s.attachAgent(c, rd, hello)
		return
	}
	if err != nil || hello.Type != "hello" {
		conn.Close()
		return
	}
	available, err := endpoint.Negotiate(hello.Peer, s.endpointPeer)
	if err != nil {
		_ = c.send(clientMsg{Type: "error", Reason: err.Error(), Peer: s.endpointPeer})
		c.close()
		return
	}
	if hello.Visible != nil && !*hello.Visible && !endpoint.Allows(available, endpoint.MachineGeneration, s.endpointPeer.Methods, "visibility") {
		_ = c.send(clientMsg{Type: "error", Reason: "endpoint_action_unsupported: visibility", Peer: s.endpointPeer})
		c.close()
		return
	}
	c.available = available
	c.terminal = hello.Terminal
	c.directGraphics = hello.GraphicsFileTransport == endpoint.DirectKittyTransport
	for _, method := range []string{endpoint.GraphicsFileMessage, endpoint.GraphicsStartedMessage, endpoint.GraphicsResultMessage, endpoint.GraphicsRetiredMessage} {
		c.directGraphics = c.directGraphics && endpoint.Allows(available, 1, hello.Methods, method) && endpoint.Allows(available, 1, s.endpointPeer.Methods, method)
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
	if s.clients == nil {
		s.clients = map[*clientConn]bool{}
	}
	s.clients[c] = true
	s.client = c
	s.lastUse++
	c.lastUse = s.lastUse
	s.mu.Unlock()
	if err := c.send(clientMsg{Type: "welcome", Peer: s.endpointPeer, Available: available}); err != nil {
		s.dropConn(c)
		return
	}
	var configID uint64
	pendingConfig := map[uint64]func(config.Config, error){}
	s.app.Do(func() {
		if !s.displayClientAttached(c) {
			return
		}
		s.sendDecisionProjection(c)
		c.viewer = s.app.NewViewer(&viewerSink{s: s, c: c}, hello.Cols, hello.Rows, func(reason string) { s.detachClient(c, reason) })
		c.viewer.SetContinuousSidebarRows(endpoint.Allows(available, 1, hello.Methods, "sidebar_continuous"))
		if hello.Visible != nil && !*hello.Visible {
			c.viewer.SetVisible(s.app, false)
		}
		c.viewer.Configure(s.app, hello.Config, hello.Preferences, func(preferences endpoint.ViewPreferences) error {
			return c.send(clientMsg{Type: "preferences", Preferences: &preferences})
		})
		if hello.ConfigWarning != "" {
			c.viewer.ConfigError(s.app, hello.ConfigWarning)
		}
		c.configEditor = func(edit config.Edit, done func(config.Config, error)) {
			if !endpoint.Allows(available, 1, hello.Methods, "config_result") {
				done(config.Config{}, fmt.Errorf("endpoint_action_unsupported: config_result"))
				return
			}
			configID++
			pendingConfig[configID] = done
			if err := c.send(clientMsg{Type: "config_edit", ConfigID: configID, ConfigEdit: &edit}); err != nil {
				delete(pendingConfig, configID)
				done(config.Config{}, err)
			}
		}
		c.viewer.SetConfigEditor(func(edit config.Edit, done func(config.Config, error)) {
			if edit.Kind == "reload" {
				// Leave the current viewer context before applying session settings.
				go s.app.Do(func() { s.reloadConfig() })
				return
			}
			c.configEditor(edit, done)
		})
		c.viewer.SetMachineRequester(func(action string) error {
			if !endpoint.Allows(available, endpoint.MachineGeneration, hello.Methods, "machine_menu") {
				return fmt.Errorf("endpoint_action_unsupported: machine_menu")
			}
			return c.send(clientMsg{Type: "machine_menu", MachineAction: action})
		})
		s.installGraphicsSnapshotSink(c, hello.Peer)
		c.viewer.Begin(s.app, hello.Xpixel, hello.Ypixel)
	})
	if c.viewer == nil {
		return
	}
	defer s.dropConn(c)
	var pendingInput *clientMsg
	var bufferedInputErr error
	for {
		if bufferedInputErr != nil {
			return
		}
		var m clientMsg
		var err error
		if pendingInput != nil {
			m, pendingInput = *pendingInput, nil
		} else {
			m, err = readClientMsg(rd)
		}
		if err != nil {
			return
		}
		if !endpoint.Allows(available, endpoint.RequiredGeneration(m.Type), s.endpointPeer.Methods, m.Type) {
			_ = c.send(clientMsg{Type: "error", Reason: "endpoint_action_unsupported: " + m.Type})
			continue
		}
		switch m.Type {
		case endpoint.GraphicsStartedMessage, endpoint.GraphicsResultMessage:
			s.app.Do(func() { s.graphicsTransmission(c, m) })
		case "control":
			s.app.Do(func() { s.graphicsControl(c, m) })
		case "view_config":
			if m.Preferences != nil && m.Preferences.Validate() != nil {
				_ = c.send(clientMsg{Type: "error", Reason: "endpoint_preferences_invalid"})
				continue
			}
			s.app.Do(func() { c.viewer.UpdatePresentation(s.app, m.Config, m.Preferences) })
		case "visibility":
			if m.Visible == nil {
				_ = c.send(clientMsg{Type: "error", Reason: "endpoint_visibility_invalid: missing visible state"})
				continue
			}
			s.app.Do(func() { c.viewer.SetVisible(s.app, *m.Visible) })
		case "workspace_select":
			s.app.Do(func() {
				if !c.viewer.SelectWorkspace(s.app, m.Workspace) {
					_ = c.send(clientMsg{Type: "error", Reason: "endpoint_workspace_not_found"})
				}
			})
		case "config_result":
			s.app.Do(func() {
				done := pendingConfig[m.ConfigID]
				if done == nil {
					return
				}
				delete(pendingConfig, m.ConfigID)
				var err error
				if m.Reason != "" {
					err = fmt.Errorf("client configuration: %s", m.Reason)
				}
				if m.Config == nil && err == nil {
					err = fmt.Errorf("client configuration: missing result")
				}
				var cfg config.Config
				if m.Config != nil {
					cfg = *m.Config
				}
				c.viewer.ConfigurationResult(s.app, done, cfg, err)
			})
		case "decision_answer":
			s.app.Do(func() {
				if m.DecisionAnswer == nil {
					_ = c.send(clientMsg{Type: "error", Reason: "decision_answer missing"})
					return
				}
				p := *m.DecisionAnswer
				p.Responder = "client"
				resp := s.decisionRequest(&api.Request{ID: "client:decision:answer", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(p)})
				if resp.Err != nil {
					_ = c.send(clientMsg{Type: "decision_answer_result", Reason: resp.Err.Code + ": " + resp.Err.Message})
				} else {
					_ = c.send(clientMsg{Type: "decision_answer_result"})
				}
			})
		case "preferences_error":
			s.app.Do(func() { c.viewer.ConfigError(s.app, m.Reason) })
		case "rendered":
			if m.Rendered != nil {
				s.app.Do(func() { c.viewer.Rendered(s.app, *m.Rendered, m.FocusEpoch) })
			}
		case "input", "paste":
			m, pendingInput, bufferedInputErr = bufferedMotions(rd, m)
			s.viewerInput(c, m)
		case "resize":
			s.app.Do(func() { c.viewer.Resize(s.app, m.Cols, m.Rows, m.Xpixel, m.Ypixel) })
		}
	}
}
