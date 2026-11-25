package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/monitor"
)

const runawayStopUnavailable = "実行ファイルを照合できないため停止できません。ペインへ移動して確認してください"

type runawayKey struct {
	pane int
	root monitor.Identity
	proc monitor.Identity
}

type runawayState struct {
	config   chan config.RunawaySettings
	alerts   map[runawayKey]runawayEntry // app loop only
	snoozed  map[runawayKey]time.Time
	sequence uint64
}

type runawayEntry struct {
	endpoint.RunawayAlert
	path string
}

func (r *runawayState) list() []endpoint.RunawayAlert {
	alerts := make([]endpoint.RunawayAlert, 0, len(r.alerts))
	for _, alert := range r.alerts {
		alerts = append(alerts, alert.RunawayAlert)
	}
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Pane != alerts[j].Pane {
			return alerts[i].Pane < alerts[j].Pane
		}
		return alerts[i].PID < alerts[j].PID
	})
	return alerts
}

func (s *Server) configureRunaway(settings config.RunawaySettings) {
	if s.runaway.config == nil {
		return
	}
	if !settings.Enabled && len(s.runaway.alerts) > 0 {
		s.runaway.alerts = nil
		s.broadcastRunaway()
	}
	select {
	case <-s.runaway.config:
	default:
	}
	s.runaway.config <- settings
}

func (s *Server) runawayRoot(id int) monitor.Identity {
	if p := s.sup.Get(id); p != nil {
		select {
		case <-p.Exited():
			return monitor.Identity{}
		default:
		}
		pid, start := p.ShellIdentity()
		return monitor.Identity{PID: pid, Start: start}
	}
	return monitor.Identity{}
}

func (s *Server) runawayRoots() monitor.Roots {
	roots := make(monitor.Roots)
	for _, space := range s.app.State().Workspaces {
		for _, pane := range s.app.State().WorkspacePaneIDs(space) {
			if root := s.runawayRoot(pane); root.PID > 1 && root.Start != "" {
				roots[pane] = root
			}
		}
	}
	return roots
}

func (s *Server) runawayLoop(settings config.RunawaySettings) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.app.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var detector monitor.Detector
	paths := make(map[runawayKey]string)
	host, _ := os.Hostname()
	host = cleanRunawayText(host, 72)
	for {
		var tick <-chan time.Time
		if settings.Enabled {
			tick = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case next := <-s.runaway.config:
			if next != settings {
				settings = next
				detector = monitor.Detector{}
				timer.Reset(0)
			}
		case <-tick:
			started := time.Now()
			nextPoll := started.Add(settings.Interval)
			sampleCtx, stop := context.WithDeadline(ctx, nextPoll)
			var roots monitor.Roots
			s.app.DoContext(sampleCtx, func() {
				if s.cfg.RunawaySettings() == settings && s.closing.Load() == 0 && !s.inputHandoffFrozen {
					roots = s.runawayRoots()
				}
			})
			rows, err := monitor.Sample(sampleCtx, roots)
			stop()
			if err == nil {
				now := time.Now()
				measurements := detector.Observe(now, settings, roots, rows)
				current := make(map[runawayKey]string)
				for _, m := range measurements {
					key := runawayKey{m.Pane, roots[m.Pane], m.Identity}
					if _, shown := paths[key]; !shown && !m.Breach {
						continue
					}
					current[key] = runawayPath(m.Identity, monitor.Executable)
				}
				paths = current
				s.app.DoContext(ctx, func() {
					if s.cfg.RunawaySettings() == settings && s.closing.Load() == 0 && !s.inputHandoffFrozen {
						s.applyRunaway(now, host, roots, measurements, current)
					}
				})
			} else {
				detector = monitor.Detector{}
			}
			timer.Reset(time.Until(nextPoll))
		}
	}
}

func runawayPath(identity monitor.Identity, executable func(int) (string, error)) string {
	path, err := executable(identity.PID)
	if err == nil && monitor.CheckTarget(monitor.Target{Identity: identity, Path: path}) == nil {
		return path
	}
	return ""
}

func cleanRunawayText(value string, limit int) string {
	var out strings.Builder
	count := 0
	for _, r := range value {
		if count == limit {
			out.WriteRune('…')
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		out.WriteRune(r)
		count++
	}
	return out.String()
}

func (s *Server) applyRunaway(now time.Time, host string, roots monitor.Roots, rows []monitor.Measurement, paths map[runawayKey]string) {
	r := &s.runaway
	next := make(map[runawayKey]runawayEntry)
	live := make(map[runawayKey]bool, len(rows))
	for _, m := range rows {
		root := roots[m.Pane]
		key := runawayKey{m.Pane, root, m.Identity}
		old, active := r.alerts[key]
		until, snoozed := r.snoozed[key]
		if !m.Breach && !active && !snoozed {
			continue
		}
		wi, _ := s.app.State().FindManagedPane(m.Pane)
		if wi < 0 || s.runawayRoot(m.Pane) != root {
			continue
		}
		live[key] = true
		path := paths[key]
		sameTarget := active && old.path == path
		if now.Before(until) || (!m.Breach && !sameTarget && !(active && path == "")) {
			continue
		}
		space := s.app.State().Workspaces[wi]
		// Never read argv for a warning. The executable name identifies
		// the process without sending credentials in arbitrary arguments.
		name := m.Name
		stopDisabledReason := runawayStopUnavailable
		if path != "" {
			name = path
			stopDisabledReason = ""
		}
		if name != "" {
			name = cleanRunawayText(filepath.Base(name), 48)
		}
		if name == "" {
			name = "PID " + fmt.Sprint(m.PID)
		}
		if !sameTarget {
			r.sequence++
			old.ID = fmt.Sprintf("%s-%d", s.generation, r.sequence)
		}
		alert := endpoint.RunawayAlert{
			ID: old.ID, Machine: host, Pane: m.Pane, Workspace: space.ID,
			SpaceName: cleanRunawayText(space.Name, 72), PID: m.PID, Name: name, Command: name + " [引数は非表示]",
			RSS: m.RSS, TreeRSS: m.TreeRSS, Spike: m.Spike, TreeSpike: m.TreeSpike,
			StopDisabledReason: stopDisabledReason, CPUPercent: m.Percent, CPUSeconds: int64(m.HighCPU / time.Second),
		}
		next[key] = runawayEntry{RunawayAlert: alert, path: path}
		if !active {
			s.notifyRunaway(alert)
		}
	}
	for key, until := range r.snoozed {
		if !live[key] || !now.Before(until) {
			delete(r.snoozed, key)
		}
	}
	before := r.list()
	r.alerts = next
	if !slices.Equal(before, r.list()) {
		s.broadcastRunaway()
	}
}

func (s *Server) notifyRunaway(alert endpoint.RunawayAlert) {
	title := fmt.Sprintf("暴走検知 s%d/p%d (%s)", alert.Workspace, alert.Pane, alert.Name)
	body := fmt.Sprintf("PID %d · %.1f MiB RSS", alert.PID, float64(alert.RSS)/1024)
	switch s.cfg.ToastDelivery() {
	case config.ToastTerminal:
		s.sendOSC9(title, body)
	case config.ToastSystem:
		for _, c := range s.displayClients() {
			if c.available >= endpoint.NotificationGeneration {
				n := &endpoint.Notification{Title: title, Body: body, Kind: "runaway", TargetPaneID: fmtPaneID(alert.Pane), SystemToast: true}
				c.post(clientMsg{Type: "notify", Notification: n})
			}
		}
	}
}

func (s *Server) broadcastRunaway() {
	msg := clientMsg{Type: endpoint.RunawayAlerts, RunawayAlerts: s.runaway.list()}
	for _, c := range s.displayClients() {
		if c.runawayCapable {
			c.post(msg)
		}
	}
}

func (s *Server) clearRunawayPane(pane int) {
	changed := false
	for key := range s.runaway.alerts {
		if key.pane == pane {
			delete(s.runaway.alerts, key)
			changed = true
		}
	}
	for key := range s.runaway.snoozed {
		if key.pane == pane {
			delete(s.runaway.snoozed, key)
		}
	}
	if changed {
		s.broadcastRunaway()
	}
}

func (s *Server) runawayAction(c *clientConn, action *endpoint.RunawayAction) error {
	return s.runawayActionWithSignal(c, action, monitor.Signal)
}

func (s *Server) runawayActionWithSignal(c *clientConn, action *endpoint.RunawayAction,
	signal func(monitor.Target, monitor.Identity, monitor.Roots, bool) error) error {
	if action == nil || action.ID == "" || !c.runawayCapable || c.viewer == nil ||
		!s.displayClientAttached(c) || !s.cfg.RunawaySettings().Enabled ||
		s.closing.Load() != 0 || s.inputHandoffFrozen {
		return errors.New("暴走検知の操作を実行できません")
	}
	for key, alert := range s.runaway.alerts {
		if alert.ID != action.ID || s.runawayRoot(key.pane) != key.root {
			continue
		}
		target := monitor.Target{Identity: key.proc, Path: alert.path}
		var err error
		if target.Path == "" {
			err = monitor.CheckIdentity(target.Identity)
		} else {
			err = monitor.CheckTarget(target)
		}
		if err != nil {
			delete(s.runaway.alerts, key)
			s.broadcastRunaway()
			return err
		}
		switch action.Kind {
		case endpoint.RunawayInspect:
			return nil
		case endpoint.RunawayTerm, endpoint.RunawayKill:
			if target.Path == "" {
				return errors.New(runawayStopUnavailable)
			}
			if err := signal(target, key.root, s.runawayRoots(), action.Kind == endpoint.RunawayKill); err != nil {
				if monitor.CheckTarget(target) != nil {
					delete(s.runaway.alerts, key)
					s.broadcastRunaway()
				}
				return err
			}
			return nil
		case endpoint.RunawayFocus:
			return c.viewer.FocusPane(s.app, key.pane)
		case endpoint.RunawaySnooze:
			if s.runaway.snoozed == nil {
				s.runaway.snoozed = make(map[runawayKey]time.Time)
			}
			s.runaway.snoozed[key] = time.Now().Add(monitor.SnoozeDuration)
			delete(s.runaway.alerts, key)
			s.broadcastRunaway()
			return nil
		}
	}
	return errors.New("対象の暴走プロセスを確認できません")
}
