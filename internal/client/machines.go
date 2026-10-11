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

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
)

type managedEndpoint struct {
	profile                  machine.Profile
	connection               *endpointConnection
	cancel                   context.CancelFunc
	attempt                  uint64
	retry                    retryPolicy
	status, reason, approval string
	snapshot                 *render.Input
	epoch                    uint64
	requestAck               bool
}

type endpointEvent struct {
	id         string
	attempt    uint64
	connection *endpointConnection
	message    msg
	err        error
	approval   string
}

type pendingMachine struct {
	id        string
	workspace int
	deadline  time.Time
	requested bool
}

type machineClient struct {
	session         string
	settings        *localSettings
	endpoints       map[string]*managedEndpoint
	order           []string
	active          string
	pending         *pendingMachine
	size            tui.Winsize
	in, out         *os.File
	events          chan endpointEvent
	ctx             context.Context
	workers         sync.WaitGroup
	sidebar         render.MachineSidebar
	frames          render.Encoder
	title           windowTitleState
	regions         *render.Output
	held            []input.Event
	suppressRelease bool
	blurred         bool
	notice          string
	noticeUntil     time.Time
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
		fmt.Fprintln(out, "gtx:", err)
		return pumpDone
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{session: session, settings: settings, endpoints: map[string]*managedEndpoint{}, order: []string{"local"}, active: "local", size: size, in: in, out: out, events: make(chan endpointEvent), ctx: ctx}
	m.endpoints["local"] = &managedEndpoint{profile: machine.Profile{ID: "local", Label: "Local", Session: session, Enabled: true}}
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
	input, stopInput, err := readMachineInput(in, ctx)
	if err != nil {
		fmt.Fprintln(out, "gtx:", err)
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
			if err := m.input(event); err != nil {
				m.notify(err.Error())
				_ = m.paint()
			}
		case <-winch:
			if size, err := tui.GetWinsize(in); err == nil {
				m.size = size
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
			m.advance(time.Now())
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
	if m.pending != nil {
		include(m.pending.deadline)
	}
	for _, e := range m.endpoints {
		include(e.retry.next)
	}
	return next
}

func (m *machineClient) advance(now time.Time) {
	if !m.noticeUntil.IsZero() && !now.Before(m.noticeUntil) {
		m.notice = ""
		m.noticeUntil = time.Time{}
	}
	if m.pending != nil && !now.Before(m.pending.deadline) {
		m.endpoints[m.pending.id].reason = "machine switch timed out"
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
