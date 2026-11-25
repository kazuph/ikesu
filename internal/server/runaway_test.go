package server

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/monitor"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRunawaySnoozeExpiryAndExit(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "runaway")
	s.cfg, _ = config.Decode([]byte("[runaway]\nenabled=true\n"))
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	root := s.runawayRoot(1)
	if root.Start == "" {
		t.Fatal("missing pane root")
	}
	roots := monitor.Roots{1: root}
	measurements := []monitor.Measurement{{Process: monitor.Process{Identity: root, Pane: 1, RSS: 8193}, Breach: true}}
	path, err := monitor.Executable(root.PID)
	if err != nil {
		t.Fatal(err)
	}
	names := map[runawayKey]string{{1, root, root}: path}
	now := time.Now()
	s.applyRunaway(now, "machine", roots, measurements, names)
	alerts := s.runaway.list()
	if len(alerts) != 1 {
		t.Fatalf("initial alert = %+v", alerts)
	}
	conn, peer := net.Pipe()
	t.Cleanup(func() { conn.Close(); peer.Close() })
	go io.Copy(io.Discard, peer)
	c := &clientConn{conn: conn, runawayCapable: true, viewer: s.app.NewViewer(nil, 80, 24, nil)}
	s.clients = map[*clientConn]bool{c: true}
	if err := s.runawayAction(c, &endpoint.RunawayAction{ID: alerts[0].ID, Kind: endpoint.RunawaySnooze}); err != nil {
		t.Fatal(err)
	}
	for key := range s.runaway.snoozed {
		s.runaway.snoozed[key] = now.Add(monitor.SnoozeDuration)
	}
	s.applyRunaway(now.Add(15*time.Minute-time.Nanosecond), "machine", roots, measurements, names)
	if len(s.runaway.alerts) != 0 {
		t.Fatal("snoozed process alerted early")
	}
	s.applyRunaway(now.Add(15*time.Minute), "machine", roots, measurements, names)
	alertAgain := s.runaway.list()
	if len(alertAgain) != 1 || alertAgain[0].ID == alerts[0].ID {
		t.Fatalf("alert should return with a fresh ID: %+v", alertAgain)
	}
	for _, id := range []string{alerts[0].ID, "unrelated-pid"} {
		if err := s.runawayAction(c, &endpoint.RunawayAction{ID: id, Kind: endpoint.RunawayKill}); err == nil {
			t.Fatalf("accepted stale or unrelated action ID: %s", id)
		}
	}
	s.applyRunaway(now.Add(16*time.Minute), "machine", roots, nil, nil)
	if len(s.runaway.alerts) != 0 || len(s.runaway.snoozed) != 0 {
		t.Fatal("exited process left stale state")
	}
}

func TestRunawaySanitizesCommand(t *testing.T) {
	t.Parallel()
	s := cleanRunawayText("node\x1b\n\u202e"+strings.Repeat("界", 40), 12)
	if s != "node   "+strings.Repeat("界", 5)+"…" {
		t.Fatalf("sanitized command = %q", s)
	}
}

func TestRunawayManagedPopupAndOverlayVisibility(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "runaway-runtime-panes")
	s.cfg, _ = config.Decode([]byte("[runaway]\nenabled=true\n"))
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	st := s.app.State()
	cwd := st.ActiveWorkspace().Cwd
	if _, err := s.app.OpenPluginPopup(cwd, []string{"/bin/cat"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	popup := st.Popup.PaneID
	if err := s.app.ExecCmd(model.CmdNewWorkspace{Cwd: cwd}); err != nil {
		t.Fatal(err)
	}
	overlay, err := s.app.OpenOverlay(st.FocusedPane(), cwd, []string{"/bin/cat"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := s.app.NewViewer(io.Discard, 80, 24, nil)
	visible := 0
	v.SetSnapshotSink(func(in render.Input) error { visible = in.State.FocusedManagedPane(); return nil })
	conn, peer := net.Pipe()
	t.Cleanup(func() { conn.Close(); peer.Close() })
	go io.Copy(io.Discard, peer)
	c := &clientConn{conn: conn, runawayCapable: true, viewer: v}
	s.clients = map[*clientConn]bool{c: true}

	for index, focus := range []int{popup, overlay} {
		want := []int{overlay, popup}[index]
		if st.FocusedManagedPane() != want {
			t.Fatalf("focus = %d, want %d before sample", st.FocusedManagedPane(), want)
		}
		roots := s.runawayRoots()
		var rows []monitor.Measurement
		paths := map[runawayKey]string{}
		for _, pane := range []int{popup, overlay} {
			root, ok := roots[pane]
			if !ok {
				t.Fatalf("runtime pane %d missing from roots", pane)
			}
			rows = append(rows, monitor.Measurement{Process: monitor.Process{Identity: root, Pane: pane}, Breach: true})
			path, err := monitor.Executable(root.PID)
			if err != nil {
				t.Fatal(err)
			}
			paths[runawayKey{pane, root, root}] = path
		}
		s.applyRunaway(time.Now(), "machine", roots, rows, paths)
		var id string
		for _, alert := range s.runaway.list() {
			if alert.Pane == focus {
				id = alert.ID
			}
		}
		if len(s.runaway.alerts) != 2 || id == "" {
			t.Fatalf("runtime pane alert missing: %+v", s.runaway.list())
		}
		if err := s.runawayAction(c, &endpoint.RunawayAction{ID: id, Kind: endpoint.RunawayFocus}); err != nil {
			t.Fatal(err)
		}
		v.Draw(s.app, nil)
		if visible != focus {
			t.Fatalf("viewer selected %d, want runtime pane %d", visible, focus)
		}
		if index == 0 {
			if err := s.app.ExecCmd(model.CmdSelectWorkspace{Index: 0}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRunawayExecInvalidatesPreviousAlert(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "runaway-exec")
	s.cfg, _ = config.Decode([]byte("[runaway]\nenabled=true\n"))
	s.sup.SetShell("/bin/sh", "non_login")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	root := s.runawayRoot(1)
	roots := s.runawayRoots()
	key := runawayKey{1, root, root}
	first, err := monitor.Executable(root.PID)
	if err != nil {
		t.Fatal(err)
	}
	rows := []monitor.Measurement{{Process: monitor.Process{Identity: root, Pane: 1}, Breach: true}}
	s.applyRunaway(time.Now(), "machine", roots, rows, map[runawayKey]string{key: first})
	alerts := s.runaway.list()
	if len(alerts) != 1 {
		t.Fatalf("missing shell alert: %+v", alerts)
	}
	conn, peer := net.Pipe()
	t.Cleanup(func() { conn.Close(); peer.Close() })
	go io.Copy(io.Discard, peer)
	c := &clientConn{conn: conn, runawayCapable: true, viewer: s.app.NewViewer(nil, 80, 24, nil)}
	s.clients = map[*clientConn]bool{c: true}
	s.sup.Input(1, []byte("exec /bin/sleep 30\n"))
	deadline := time.Now().Add(2 * time.Second)
	var next string
	for time.Now().Before(deadline) {
		next, err = monitor.Executable(root.PID)
		if err == nil && next != first && filepath.Base(next) == "sleep" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if filepath.Base(next) != "sleep" || s.runawayRoot(1) != root {
		t.Fatalf("fixture did not exec with the same PID and birth time: %s", next)
	}
	if err := s.runawayAction(c, &endpoint.RunawayAction{ID: alerts[0].ID, Kind: endpoint.RunawayInspect}); err == nil {
		t.Fatal("opened stale details after exec")
	}
	err = s.runawayAction(c, &endpoint.RunawayAction{ID: alerts[0].ID, Kind: endpoint.RunawayKill})
	if err == nil || len(s.runaway.alerts) != 0 || monitor.CheckTarget(monitor.Target{Identity: root, Path: next}) != nil {
		t.Fatalf("stale action accepted or replacement exited: %v, %+v", err, s.runaway.list())
	}
	// Republish after sampling the changed executable and verify that
	// the previous identifier cannot be used for the replacement.
	s.applyRunaway(time.Now(), "machine", roots, rows, map[runawayKey]string{key: next})
	replacement := s.runaway.list()
	if len(replacement) != 1 || replacement[0].ID == alerts[0].ID || replacement[0].Name != "sleep" {
		t.Fatalf("replacement alert: %+v", replacement)
	}
}

func TestRunawayRelativeExecutableWarnsWithAbsolutePath(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "runaway-relative")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	if _, err := s.app.OpenPluginPopup("/bin", []string{"./sleep", "30"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	pane := s.app.State().Popup.PaneID
	roots := s.runawayRoots()
	root := roots[pane]
	path := runawayPath(root, monitor.Executable)
	if !filepath.IsAbs(path) || filepath.Base(path) != "sleep" {
		t.Fatalf("relative start resolved to %q", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		rows, err := monitor.Sample(ctx, roots)
		if err != nil {
			t.Fatal(err)
		}
		var d monitor.Detector
		measurements := d.Observe(time.Now(), config.RunawaySettings{MemoryKiB: 1, SpikeKiB: 1024, CPUDuration: time.Minute}, roots, rows)
		s.applyRunaway(time.Now(), "host", roots, measurements, map[runawayKey]string{{pane, root, root}: path})
		for _, alert := range s.runaway.list() {
			if alert.Pane == pane && alert.PID == root.PID && alert.Name == "sleep" && alert.RSS > 1 && alert.StopDisabledReason == "" {
				return
			}
		}
		if ctx.Err() != nil {
			t.Fatalf("relative process was not detected: %+v", s.runaway.list())
		}
	}
}

func TestRunawayLookupFailureStillWarnsAndNeverSignals(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "runaway-path-failure")
	s.cfg, _ = config.Decode([]byte("[runaway]\nenabled=true\n"))
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	roots := s.runawayRoots()
	root := roots[1]
	path := runawayPath(root, func(int) (string, error) { return "", os.ErrPermission })
	measurement := monitor.Measurement{Process: monitor.Process{Identity: root, Pane: 1, RSS: 100, Name: "blocked-sleep"}, Breach: true}
	s.applyRunaway(time.Now(), "host", roots, []monitor.Measurement{measurement}, map[runawayKey]string{{1, root, root}: path})
	alerts := s.runaway.list()
	if len(alerts) != 1 || alerts[0].Name != "blocked-sleep" || alerts[0].RSS != 100 || alerts[0].StopDisabledReason == "" {
		t.Fatalf("unresolvable program must still produce a warning: %+v", alerts)
	}
	conn, peer := net.Pipe()
	t.Cleanup(func() { conn.Close(); peer.Close() })
	go io.Copy(io.Discard, peer)
	c := &clientConn{conn: conn, runawayCapable: true, viewer: s.app.NewViewer(io.Discard, 80, 24, nil)}
	s.clients = map[*clientConn]bool{c: true}
	sends := 0
	signal := func(monitor.Target, monitor.Identity, monitor.Roots, bool) error { sends++; return nil }
	for _, kind := range []string{endpoint.RunawayTerm, endpoint.RunawayKill} {
		err := s.runawayActionWithSignal(c, &endpoint.RunawayAction{ID: alerts[0].ID, Kind: kind}, signal)
		if err == nil || err.Error() != alerts[0].StopDisabledReason {
			t.Fatalf("unresolvable %s: %v", kind, err)
		}
	}
	for _, kind := range []string{endpoint.RunawayInspect, endpoint.RunawayFocus, endpoint.RunawaySnooze} {
		err := s.runawayActionWithSignal(c, &endpoint.RunawayAction{ID: alerts[0].ID, Kind: kind}, signal)
		if err != nil {
			t.Fatalf("%s should still work: %v", kind, err)
		}
	}
	if sends != 0 {
		t.Fatalf("unresolvable target sent %d signals", sends)
	}
}
