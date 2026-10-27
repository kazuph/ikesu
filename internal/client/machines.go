package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/latency"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

type managedEndpoint struct {
	snapshotAttempt          uint64
	boot                     string
	textOnly                 bool
	mouse                    hostMouseState
	profile                  machine.Profile
	connection               *endpointConnection
	cancel                   context.CancelFunc
	ctx                      context.Context
	attempt                  uint64
	retry                    retryPolicy
	status, reason, approval string
	snapshot                 *render.Input
	epoch                    uint64
	requestAck               bool
	hostKbdFlags             int
	removed                  bool
	paste                    *clipboardPaste
	restarting               bool
	restartInput             []byte
}

type endpointEvent struct {
	id             string
	attempt        uint64
	connection     *endpointConnection
	message        msg
	err            error
	approval       string
	catalogChanged bool
	formUpdate     func()
	configRevision uint64
}

type pendingMachine struct {
	sidebar            *machineSidebarAction
	keys               []input.Event
	id                 string
	workspace          int
	deadline           time.Time
	requested          bool
	create             bool
	previousWorkspaces map[int]bool
}

type machineClient struct {
	koiNext           time.Time
	koiClock          render.KoiAnimation
	koiVisible        bool
	koiKitty          bool
	koiRetained       bool
	koiInput          render.Input
	koiScene          render.KoiScene
	clientVersion     string
	trace             *latency.Trace
	drawSeq, probeSeq uint64
	probeUntil        time.Time
	probeEndpoint     string
	probeGeneration   uint64
	probeUnanswered   uint64
	probeDiscardNext  bool
	nativeTransfer    *nativeViewerTransfer
	fileGraphics      graphicsFileReceiver
	mouse             hostMouseState
	hostKbdFlags      int
	inputSource       platform.InputSource
	session           string
	configEdits       []pendingConfigEdit
	configEditMu      sync.Mutex
	configEditing     bool
	configRevision    uint64
	settings          *localSettings
	endpoints         map[string]*managedEndpoint
	order             []string
	active            string
	pending           *pendingMachine
	drag              *machineDrag
	size              tui.Winsize
	in, out           *os.File
	events            chan endpointEvent
	ctx               context.Context
	workers           sync.WaitGroup
	sidebar           render.MachineSidebar
	frames            render.Encoder
	composer          render.Composer
	outputFence       render.OutputFence
	paintQueued       bool
	inputBatch        bool
	title             windowTitleState
	regions           *render.Output
	tabFeedback       *tabFeedback
	held              []input.Event
	suppressRelease   bool
	blurred           bool
	notice            string
	noticeUntil       time.Time
	forms             *tui.MachineForms
	readClipboard     func(context.Context) (clipboardContent, error)
}

func savedMachines(session string) []machine.Profile {
	return (machine.Catalog{Path: filepath.Join(api.SessionDir(session), "machines.json")}).Load()
}

func hasEnabledMachine(profiles []machine.Profile) bool {
	for _, p := range profiles {
		if p.Enabled {
			return true
		}
	}
	return false
}

func pumpMachines(session string, local net.Conn, in, out *os.File, settings *localSettings, profiles []machine.Profile) pumpResult {
	size, err := tui.GetWinsize(in)
	if err != nil {
		fmt.Fprintln(out, "ikesu:", err)
		return pumpDone
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{clientVersion: settings.clientVersion, session: session, settings: settings, endpoints: map[string]*managedEndpoint{}, order: []string{"local"}, active: "local", size: size, in: in, out: out, events: make(chan endpointEvent), ctx: ctx}
	if latency.Enabled {
		m.trace = latency.Open(filepath.Join(api.SessionDir(session), "client.log"))
	}
	defer m.trace.Close()
	m.koiKitty = kitty.SupportsImages(in, out)
	m.endpoints["local"] = &managedEndpoint{profile: machine.Profile{ID: "local", Label: "Local", Session: session, Enabled: true}}
	m.forms = tui.NewMachineForms(ctx, m.catalogPath(), func(fn func()) { m.post(ctx, endpointEvent{formUpdate: fn}) })
	defer m.forms.Close()
	defer func() { _, _ = out.Write(m.frames.RestoreCursor()) }()
	defer m.inputSource.Prefix(false, false)
	for _, p := range profiles {
		if p.Enabled {
			m.endpoints[p.ID] = &managedEndpoint{profile: p}
			m.order = append(m.order, p.ID)
		}
	}
	defer func() {
		cancel()
		for _, e := range m.endpoints {
			if e.cancel != nil {
				e.cancel()
			}
		}
		m.workers.Wait()
	}()
	m.start("local", local)
	for _, id := range m.order[1:] {
		m.start(id, nil)
	}
	stopCatalog, err := watchMachineCatalog(ctx, m.catalogPath(), func(ctx context.Context, err error) {
		m.post(ctx, endpointEvent{catalogChanged: true, err: err})
	})
	if err != nil {
		fmt.Fprintln(out, "ikesu: machine catalog watch:", err)
		return pumpRejected
	}
	defer stopCatalog()
	if err := m.refreshMachineCatalog(); err != nil {
		m.notify(err.Error())
	}
	input, stopInput, err := readMachineInput(in, ctx, true)
	if err != nil {
		fmt.Fprintln(out, "ikesu:", err)
		return pumpDone
	}
	defer stopInput()
	winch := make(chan os.Signal, 4)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	timer := time.NewTimer(initialRetryDelay)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		next := m.nextDeadline()
		var alarm <-chan time.Time
		if !next.IsZero() {
			timer.Reset(max(time.Duration(0), time.Until(next)))
			alarm = timer.C
		}
		select {
		case event, ok := <-input:
			if !ok {
				return pumpDone
			}
			if _, err := m.drainInput(event, input); err != nil {
				m.notify(err.Error())
				_ = m.paint()
			}
		case <-winch:
			if size, err := tui.GetWinsize(in); err == nil {
				m.size = size
				m.mouse.Geometry(size)
				if e := m.endpoints[m.active]; e.connection != nil {
					_ = e.connection.enqueue(resizeMessage(size))
				}
				if err := m.paint(); err != nil {
					return pumpDone
				}
			}
		case event := <-m.events:
			if result, done := m.event(event); done {
				return result
			}
		case <-alarm:
			now := time.Now()
			if m.koiVisible && !m.paintQueued && !m.koiNext.IsZero() && !now.Before(m.koiNext) && m.nextDeadline().Equal(m.koiNext) {
				if err := m.paintKoi(now); err != nil {
					return pumpDone
				}
				continue
			}
			m.advance(now)
			if err := m.paint(); err != nil {
				return pumpDone
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

func resizeMessage(size tui.Winsize) msg {
	return msg{Type: "resize", Cols: size.Cols, Rows: size.Rows, Xpixel: size.Xpixel, Ypixel: size.Ypixel}
}

func (m *machineClient) notify(text string) {
	m.notice = text
	m.noticeUntil = time.Now().Add(connectionNotice)
}

func (m *machineClient) nextDeadline() time.Time {
	var next time.Time
	include := func(t time.Time) {
		if !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	include(m.noticeUntil)
	if !m.paintQueued {
		include(m.koiNext)
	}
	if m.paintQueued {
		include(m.outputFence.Deadline())
	}
	if m.pending != nil {
		include(m.pending.deadline)
		if len(m.pending.keys) > 0 {
			include(m.pending.inputDeadline())
		}
	}
	for _, e := range m.endpoints {
		include(e.retry.next)
	}
	return next
}

func (m *machineClient) advance(now time.Time) {
	m.traceProbeTimeout(now)
	if !m.noticeUntil.IsZero() && !now.Before(m.noticeUntil) {
		m.notice = ""
		m.noticeUntil = time.Time{}
	}
	if m.pending != nil && !now.Before(m.pending.inputDeadline()) {
		m.pending.keys = nil
	}
	if m.pending != nil && !now.Before(m.pending.deadline) {
		m.endpoints[m.pending.id].reason = "machine switch timed out"
		if m.drag != nil && m.drag.started {
			m.notify("the space could not be moved: its machine is not ready")
		}
		m.drag = nil
		m.pending = nil
	}
	for id, e := range m.endpoints {
		if retry, notify := e.retry.due(now); retry {
			if notify {
				m.notify(e.profile.Label + ": " + e.reason)
			}
			m.start(id, nil)
		}
	}
}
