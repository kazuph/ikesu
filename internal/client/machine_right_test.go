package client

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/server"
	"github.com/kazuph/goterrace/internal/teststate"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestMachineRightServerHelper(t *testing.T) {
	session := os.Getenv("GTX_RIGHT_SERVER_TEST")
	if session == "" {
		return
	}
	code, err := server.Run(session, "right-sidebar-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

type rightMachineHarness struct {
	m    *machineClient
	t    *testing.T
	dirs map[string]string
}

func (h *rightMachineHarness) api(id, method string, params any) {
	h.t.Helper()
	conn, err := net.DialTimeout("unix", api.APISocketPath(h.dirs[id]), time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := api.WriteRequest(conn, api.Request{ID: "right-test", Method: method, Params: api.MarshalParams(params)}); err != nil {
		h.t.Fatal(err)
	}
	response, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil || response.Err != nil {
		h.t.Fatalf("%s: %+v %v", method, response, err)
	}
}
func (h *rightMachineHarness) until(label string, pred func() bool) {
	h.t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !pred() {
		select {
		case event := <-h.m.events:
			if event.err != nil {
				h.t.Fatalf("%s endpoint=%s: %v", label, event.id, event.err)
			}
			if _, done := h.m.event(event); done {
				h.t.Fatal("endpoint ended", label)
			}
			if err := h.m.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")}); err != nil {
				h.t.Fatal(err)
			}
		case <-timer.C:
			h.t.Fatalf("timeout %s active=%s pending=%+v", label, h.m.active, h.m.pending)
		}
	}
}
func newRightMachineHarness(t *testing.T) *rightMachineHarness {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "gtx-right-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg, err := config.Decode([]byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui]\nagent_panel_scope='all'\n[ui.sound]\nenabled=false\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[ui.sound]\nenabled=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(root, "terminal-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{ctx: ctx, session: "right-local", active: "local", order: []string{"local", "remote"}, out: out, settings: &localSettings{config: &cfg, path: filepath.Join(root, "preferences.json")}, size: tui.Winsize{Cols: 160, Rows: 35}, events: make(chan endpointEvent, 128), endpoints: map[string]*managedEndpoint{}}
	h := &rightMachineHarness{m: m, t: t, dirs: map[string]string{}}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var children []*exec.Cmd
	t.Cleanup(func() {
		cancel()
		for _, e := range m.endpoints {
			if e.connection != nil {
				e.connection.stream.Close()
			}
		}
		for id := range h.dirs {
			h.api(id, "server.stop", nil)
		}
		for _, cmd := range children {
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				t.Error("test server did not stop")
			}
		}
	})
	for _, id := range m.order {
		session := "right-" + id
		dir := filepath.Join(root, session)
		h.dirs[id] = dir
		log, err := os.Create(filepath.Join(root, id+".log"))
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		cmd := exec.Command(exe, "-test.run=^TestMachineRightServerHelper$")
		cmd.Env = append(os.Environ(), "GTX_RIGHT_SERVER_TEST="+session, "GTX_STATE_DIR="+root, "GTX_CONFIG="+cfgPath, "GTX_DISABLE_SOUND=1", "SHELL=/bin/sh")
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
		var stream net.Conn
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			stream, err = net.Dial("unix", api.ClientSocketPath(dir))
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		stream.SetDeadline(time.Now().Add(10 * time.Second))
		connection, err := connectEndpoint(stream, id != "local", m.size, m.settings, "")
		if err != nil {
			t.Fatal(err)
		}
		stream.SetDeadline(time.Time{})
		m.endpoints[id] = &managedEndpoint{profile: machine.Profile{ID: id, Label: id, Enabled: true, Session: session}, connection: connection, ctx: ctx, status: "online"}
		go connection.writeQueued(ctx)
		go func(id string, c *endpointConnection) {
			for {
				message, err := c.read()
				if err != nil {
					return
				}
				select {
				case m.events <- endpointEvent{id: id, message: message}:
				case <-ctx.Done():
					return
				}
			}
		}(id, connection)
	}
	h.until("both snapshots", func() bool { return m.endpoints["local"].snapshot != nil && m.endpoints["remote"].snapshot != nil })
	for _, id := range m.order {
		h.api(id, api.MethodWorkspaceCreate, map[string]any{"focus": false})
		h.api(id, api.MethodPaneSplit, map[string]any{"target_pane_id": "p1", "direction": "right", "focus": false})
		for pane := 1; pane <= 3; pane++ {
			h.api(id, api.MethodPaneReportAgent, map[string]any{"pane_id": fmt.Sprintf("p%d", pane), "agent": "codex", "source": "codex", "session_id": t.Name() + id, "state": "idle"})
		}
		h.api(id, api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: id + " job", Argv: []string{"/bin/sh", "-c", "printf right-job"}})
	}
	h.until("real panes and jobs", func() bool {
		for _, e := range m.endpoints {
			if len(e.snapshot.State.Workspaces) != 2 || len(e.snapshot.Jobs) == 0 || len(render.RightCompactRows(*e.snapshot)) != 5 {
				return false
			}
		}
		return true
	})
	// Read output from a shell in a kernel PTY on each endpoint.
	for _, id := range m.order {
		h.api(id, api.MethodPaneSendText, map[string]any{"pane_id": "p1", "text": "printf 'RIGHT_PTY_%s\\n' \"$(stty size)\"\n"})
	}
	h.until("kernel PTY output", func() bool {
		for _, e := range m.endpoints {
			var text strings.Builder
			for _, row := range e.snapshot.Panes[1].Grid {
				for _, c := range row {
					text.WriteString(c.Content)
				}
			}
			if !regexp.MustCompile(`RIGHT_PTY_[0-9]+ [0-9]+`).MatchString(text.String()) {
				return false
			}
		}
		return true
	})
	return h
}

func (h *rightMachineHarness) modes(left, right, cols, rows int) {
	h.t.Helper()
	h.m.size = tui.Winsize{Cols: cols, Rows: rows}
	pref := endpoint.ViewPreferences{SidebarWidth: 26, SidebarCollapsed: left > 0, SidebarHidden: left == 2}
	if right == 1 {
		pref.RightSidebarMode = "compact"
	}
	if right == 2 {
		pref.RightSidebarMode = "hidden"
	}
	for _, e := range h.m.endpoints {
		e.connection.enqueue(resizeMessage(h.m.size))
		e.connection.enqueue(msg{Type: "view_config", Config: h.m.settings.config, Preferences: &pref})
	}
	h.until("modes", func() bool {
		for _, e := range h.m.endpoints {
			s := e.snapshot.State
			if s.Cols != cols || s.Rows != rows || s.SidebarVisible != (left == 0) || s.SidebarHidden != (left == 2) || s.RightSidebarMode != pref.RightSidebarMode {
				return false
			}
		}
		return true
	})
	h.m.outputFence.Accept([]byte("\x1b[0n"))
	if err := h.m.paint(); err != nil {
		h.t.Fatal(err)
	}
}
func TestRightSidebarTwoMachinesActualPTYMatrixAndWheel(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	for left := 0; left < 3; left++ {
		for right := 0; right < 3; right++ {
			h.modes(left, right, 160, 35)
			var hits []render.MachineHit
			for _, hit := range m.sidebar.Hits {
				if hit.Right {
					hits = append(hits, hit)
				}
			}
			if right == 2 {
				if len(hits) != 0 {
					t.Fatal("hidden right retained click targets")
				}
				continue
			}
			frame := render.ComposeClient(*m.endpoints[m.active].snapshot, &m.sidebar, "test", false).Frame
			for _, hit := range hits {
				if right == 1 && (hit.Header || frame.Cell(hit.Rect.X, hit.Rect.Y).Content == "▾") {
					t.Fatal("compact machine header overwrote status")
				}
				if hit.Header || hit.Row.Kind != render.RowAgent && (hit.Row.Kind != render.RowJob || right != 1) {
					continue
				}
				pane := hit.Row.Pane
				if hit.Row.Kind == render.RowJob {
					pane = 1
				}
				event := mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))
				if err := m.input(event); err != nil {
					t.Fatal(err)
				}
				h.until("machine row selection", func() bool {
					return m.pending == nil && m.active == hit.Machine && m.endpoints[hit.Machine].snapshot.State.FocusedPane() == pane
				})
				if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dm", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	autoCols := 26 + model.MinimumPaneAreaWidth + model.SidebarCollapsedWidth
	h.modes(0, 0, autoCols, 6)
	for _, e := range m.endpoints {
		if e.snapshot.State.RightSidebarArea().W != 2 || e.snapshot.State.RightSidebarMode != "" {
			t.Fatal("automatic compact changed saved mode")
		}
	}
	before := m.sidebar.Scroll
	m.outputFence.Accept([]byte("\x1b[0n"))
	if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<65;%d;1M", autoCols))); err != nil {
		t.Fatal(err)
	}
	if m.sidebar.RightScroll != 1 || m.sidebar.Scroll != before {
		t.Fatal("right wheel changed left scroll or missed right", m.sidebar.RightScroll, m.sidebar.Scroll)
	}
	// All remaining visible compact cells still select their actual endpoint/pane.
	for _, hit := range append([]render.MachineHit(nil), m.sidebar.Hits...) {
		if !hit.Right || hit.Row.Kind != render.RowAgent {
			continue
		}
		if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
			t.Fatal(err)
		}
		h.until("auto compact row", func() bool {
			return m.pending == nil && m.active == hit.Machine && m.endpoints[hit.Machine].snapshot.State.FocusedPane() == hit.Row.Pane
		})
		m.suppressRelease = false
	}
}
