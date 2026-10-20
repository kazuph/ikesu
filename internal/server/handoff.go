package server

import (
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/tui"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// The isolated herdr 0.2.30 observation times out an unresponsive importer
// after 30 seconds; preserve that externally visible failure window.
const handoffWait = 30 * time.Second

type handoffIdentity struct {
	Protocol int
	Version  string
}
type handoffState struct {
	App                   tui.HandoffState
	Panes                 []runtime.PaneTransfer
	PaneMetadata          map[int]*paneMetadata
	WorkspaceMetadata     map[int]map[string]*workspaceMetadata
	ReportSources         map[int]string
	PluginPanes           map[int]pluginPaneOwner
	ObservedStart         map[int]time.Time
	LastStatus, LastAgent map[int]string
	NotifyLast            map[int]map[string]time.Time
	LastAttention         time.Time
	Journal               []api.EventEnvelope
	AgentLaunches         map[int]launchTransfer
}
type launchTransfer struct{ Started, ReadySince time.Time }
type handoffImport struct {
	State   handoffState
	Control net.Conn
	Encoder *gob.Encoder
	Decoder *gob.Decoder
	Files   []*os.File
}
type handoffResult struct {
	State string
	Error string
}

func exportListener(ln net.Listener) (*os.File, error) {
	raw, err := ln.(*net.UnixListener).SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var dupErr error
	if err = raw.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		return nil, err
	}
	if dupErr != nil {
		return nil, dupErr
	}
	return os.NewFile(uintptr(fd), "handoff-listener"), nil
}

func inheritedListener(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "handoff-listener")
	defer f.Close()
	ln, err := net.FileListener(f)
	if u, ok := ln.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false)
	}
	return ln, err
}
func RunHandoffImport(session, version string) (int, error) {
	f := os.NewFile(3, "handoff-control")
	c, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return 1, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(handoffWait))
	enc, dec := gob.NewEncoder(c), gob.NewDecoder(c)
	if err = enc.Encode(handoffIdentity{api.Protocol, version}); err != nil {
		return 1, err
	}
	in := &handoffImport{Control: c, Encoder: enc, Decoder: dec}
	if err = dec.Decode(&in.State); err != nil {
		return 1, err
	}
	for i := range in.State.Panes {
		in.Files = append(in.Files, os.NewFile(uintptr(7+i), "handoff-pty"))
	}
	return runEndpoint(session, version, endpoint.Current(), in)
}

// The app loop remains frozen during the transaction. The retiring server
// owns its descriptors until the importer has accepted state and started.
func (s *Server) liveHandoff(req *api.Request) api.Response {
	var p api.ServerLiveHandoffParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if !s.handoffMu.TryLock() {
		return api.Failure(req.ID, "handoff_failed", "handoff already in progress")
	}
	defer s.handoffMu.Unlock()
	exe, err := os.Executable()
	if p.ImportExe != nil {
		exe = *p.ImportExe
	}
	if err != nil {
		return api.Failure(req.ID, "handoff_failed", err.Error())
	}
	var handoffErr error
	s.app.Do(func() { handoffErr = s.transferHandoff(exe, p) })
	if handoffErr != nil {
		return api.Failure(req.ID, "handoff_failed", handoffErr.Error())
	}
	return api.Success(req.ID, api.OkResult{Type: "ok"})
}

func (s *Server) transferHandoff(exe string, p api.ServerLiveHandoffParams) (err error) {
	panes, files, err := s.sup.PauseHandoff()
	if err != nil {
		return err
	}
	transferred := false
	defer func() {
		for _, f := range files {
			f.Close()
		}
		if !transferred {
			s.sup.ResumeHandoff()
		}
	}()
	syscall.ForkLock.RLock()
	pairs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return err
	}
	unix.CloseOnExec(pairs[0])
	unix.CloseOnExec(pairs[1])
	syscall.ForkLock.RUnlock()
	parent := os.NewFile(uintptr(pairs[0]), "handoff-control")
	child := os.NewFile(uintptr(pairs[1]), "handoff-control")
	defer child.Close()
	control, err := net.FileConn(parent)
	parent.Close()
	if err != nil {
		return err
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(handoffWait))
	enc, dec := gob.NewEncoder(control), gob.NewDecoder(control)
	apiFile, err := exportListener(s.apiLn)
	if err != nil {
		return err
	}
	defer apiFile.Close()
	clientFile, err := exportListener(s.clientLn)
	if err != nil {
		return err
	}
	defer clientFile.Close()
	cmd := childenv.Command(exe, "--session", s.session, "server", "--handoff-import")
	cmd.Env = childenv.Scrub(os.Environ())
	cmd.Dir = s.cwd
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = append([]*os.File{child, apiFile, clientFile, s.lock}, files...)
	if err = cmd.Start(); err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) {
			message := errno.Error()
			return fmt.Errorf("failed to spawn handoff import server at %s: %s (os error %d)", exe, strings.ToUpper(message[:1])+message[1:], errno)
		}
		return fmt.Errorf("failed to spawn handoff import server at %s: %w", exe, err)
	}
	child.Close()
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	defer func() {
		if !transferred {
			_ = cmd.Process.Kill()
			<-processDone
		}
	}()
	var identity handoffIdentity
	if err = dec.Decode(&identity); err != nil {
		return handoffStreamError(err)
	}
	if p.ExpectedProtocol != nil && identity.Protocol != int(*p.ExpectedProtocol) || p.ExpectedVersion != nil && identity.Version != *p.ExpectedVersion || identity.Protocol != api.Protocol {
		return fmt.Errorf("handoff stream closed while reading line")
	}
	state := handoffState{App: s.app.HandoffState(), Panes: panes, PaneMetadata: s.paneMetadata, WorkspaceMetadata: s.workspaceMetadata, ReportSources: s.reportSources, PluginPanes: s.pluginPanes, ObservedStart: s.observedStart, LastStatus: s.lastStatus, LastAgent: s.lastAgent, NotifyLast: s.notifyLast, LastAttention: s.lastAttention, Journal: s.journal}
	state.AgentLaunches = map[int]launchTransfer{}
	for id, l := range s.agentLaunches {
		state.AgentLaunches[id] = launchTransfer{l.started, l.readySince}
	}
	if err = enc.Encode(state); err != nil {
		return err
	}
	var result handoffResult
	if err = dec.Decode(&result); err != nil {
		return handoffStreamError(err)
	}
	if result.State != "ready" {
		return fmt.Errorf("handoff import failed: %s", result.Error)
	}
	// Stop accepting on old descriptors; File duplicates remain available for rollback.
	s.apiLn.(*net.UnixListener).SetUnlinkOnClose(false)
	s.clientLn.(*net.UnixListener).SetUnlinkOnClose(false)
	s.apiLn.Close()
	s.clientLn.Close()
	restartListeners := func() {
		s.apiLn, _ = net.FileListener(apiFile)
		s.clientLn, _ = net.FileListener(clientFile)
		if s.apiLn != nil {
			go s.apiLoop()
		}
		if s.clientLn != nil {
			go s.clientLoop()
		}
	}
	if err = enc.Encode(handoffResult{State: "commit"}); err != nil {
		restartListeners()
		return err
	}
	if err = dec.Decode(&result); err != nil {
		restartListeners()
		return handoffStreamError(err)
	}
	if result.State != "running" {
		restartListeners()
		return fmt.Errorf("handoff import failed: %s", result.Error)
	}
	transferred = true
	s.handedOff = true
	s.closing.Store(2)
	s.stopDecisions()
	s.notifyRestart()
	s.sup.ReleaseHandoff()
	s.app.RetireHandoff()
	return nil
}
func handoffStreamError(err error) error {
	if os.IsTimeout(err) {
		return fmt.Errorf("timed out waiting for handoff import connection")
	}
	return fmt.Errorf("handoff stream closed while reading line")
}

func (s *Server) restoreLive(in *handoffImport, socket string) error {
	if err := s.sup.ImportHandoff(in.State.Panes, in.Files); err != nil {
		return err
	}
	// Older gob reports omit Source; restore it without treating this as a new report.
	for pid, report := range in.State.App.Reported {
		if report.Source == "" {
			report.Source = in.State.ReportSources[pid]
			in.State.App.Reported[pid] = report
		}
	}
	s.app.RestoreHandoff(in.State.App)
	s.paneMetadata, s.workspaceMetadata, s.reportSources = in.State.PaneMetadata, in.State.WorkspaceMetadata, in.State.ReportSources
	s.pluginPanes, s.observedStart = in.State.PluginPanes, in.State.ObservedStart
	s.lastStatus, s.lastAgent, s.notifyLast, s.lastAttention = in.State.LastStatus, in.State.LastAgent, in.State.NotifyLast, in.State.LastAttention
	s.journal = in.State.Journal
	s.agentLaunches = map[int]*agentLaunch{}
	for id, l := range in.State.AgentLaunches {
		s.agentLaunches[id] = &agentLaunch{l.Started, l.ReadySince}
	}
	if err := in.Encoder.Encode(handoffResult{State: "ready"}); err != nil {
		return err
	}
	var result handoffResult
	if err := in.Decoder.Decode(&result); err != nil {
		return err
	}
	if result.State != "commit" {
		return fmt.Errorf("handoff not committed")
	}
	if err := s.sup.RebindGuardSocket(socket); err != nil {
		return err
	}
	s.sup.StartImported()
	return nil
}
