package client

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestRunawayMemoryFixture(t *testing.T) {
	path := os.Getenv("IKESU_TEST_RUNAWAY_MEMORY")
	if path == "" {
		return
	}
	if err := os.WriteFile(path+".pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	rss := make([]byte, 48*1024*1024)
	for i := 0; i < len(rss); i += 4096 {
		rss[i] = 1
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	defer signal.Stop(term)
	fmt.Printf("runaway child ready: %s\n", path)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer runtime.KeepAlive(rss)
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
				return
			}
			continue
		case <-term:
		}
		if err := os.WriteFile(path, []byte("SIGTERM\n"), 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("runaway child received SIGTERM: %s\n", path)
		if filepath.Base(path) == "term" {
			return
		}
	}
}

// The real detector needs one minimum-interval poll to discover the fixtures
// and another to confirm their exits. Race process startup can take the test over five seconds.
func TestRunawayRealViewerActions(t *testing.T) {
	h := newRightMachineHarnessConfigured(t, func(string) string {
		return "\n[runaway]\nenabled=true\npoll_interval_seconds=2\nmemory_mb=40\n"
	}, true)
	m := h.m
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	targets := []string{"hold", "term", "snooze"}
	var commands []string
	for _, target := range targets {
		command := machine.Command([]string{"env", "IKESU_TEST_RUNAWAY_MEMORY=" + filepath.Join(dir, target), exe, "-test.run=^TestRunawayMemoryFixture$", "runaway-test-" + target, "--token=runaway-secret-token", "API_KEY=runaway-secret-key"})
		commands = append(commands, command+" &")
	}
	h.api("remote", api.MethodPaneSendText, api.PaneSendTextParams{PaneID: "p2", Text: strings.Join(commands, " ") + " wait\n"})
	h.until("three live processes", func() bool { return len(m.endpoints["remote"].runawayAlerts) == 3 })
	h.paintNow()

	status := h.request("remote", api.MethodServerRunaway, api.EmptyParams{})
	if status.Err != nil {
		t.Fatal(status.Err)
	}
	wire, err := json.Marshal(m.endpoints["remote"].runawayAlerts)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{string(status.Result), string(wire)} {
		if strings.Contains(payload, "runaway-secret-") || !strings.Contains(payload, "引数は非表示") {
			t.Fatalf("masked alert payload = %s", payload)
		}
	}
	readAlert := func(label string) (endpoint.RunawayAlert, bool) {
		raw, err := os.ReadFile(filepath.Join(dir, label+".pid"))
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range m.endpoints["remote"].runawayAlerts {
			if a.PID == pid {
				return a, true
			}
		}
		return endpoint.RunawayAlert{}, false
	}
	click := func(r layout.Rect) {
		t.Helper()
		if r.W == 0 || r.H == 0 {
			t.Fatal("missing click target")
		}
		for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
			if err := m.input(input.Event{Kind: kind, Mouse: uv.Mouse{Button: uv.MouseLeft, X: r.X, Y: r.Y}}); err != nil {
				t.Fatal(err)
			}
		}
		h.paintNow()
	}
	key := func(code rune) {
		t.Helper()
		if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: code}}); err != nil {
			t.Fatal(err)
		}
		h.paintNow()
	}
	choose := func(label string) {
		t.Helper()
		a, ok := readAlert(label)
		if !ok {
			t.Fatalf("no alert for %s: %+v", label, m.endpoints["remote"].runawayAlerts)
		}
		if m.runawayPainted.id == "" {
			click(m.regions.ToastRect)
			h.until("checked alert opened", func() bool { return m.runawayPainted.id != "" })
		}
		for range targets {
			if m.runawayPainted.id == a.ID {
				if len(m.regions.DialogActions) < 4 {
					t.Fatalf("dialog action rows missing: %+v", m.regions.DialogActions)
				}
				return
			}
			key('5')
			h.until("checked next alert", func() bool { return m.runawayInspect.id == "" })
			h.paintNow()
		}
		t.Fatalf("could not select alert for %s", label)
	}

	choose("hold")
	if m.active != "local" || m.runawayPainted.machine != "remote" {
		t.Fatalf("remote alert was not actionable from local: active=%s dialog=%+v", m.active, m.runawayPainted)
	}
	wide := m.size
	stale := m.regions.DialogActions[1]
	m.size = tui.Winsize{Cols: 24, Rows: 6}
	// The previous frame may still be the last acknowledged frame.
	m.runawayInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '2'}})
	m.runawayInput(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{Button: uv.MouseLeft, X: stale.X, Y: stale.Y}})
	h.paintNow()
	if len(m.regions.DialogActions) != 0 {
		t.Fatalf("narrow dialog exposed unsafe actions: %+v", m.regions.DialogActions)
	}
	key('1')
	key('2')
	if hold, _ := readAlert("hold"); syscall.Kill(hold.PID, 0) != nil {
		t.Fatal("hidden action killed the process")
	}
	if _, err := os.Stat(filepath.Join(dir, "hold")); !os.IsNotExist(err) {
		t.Fatalf("hidden action sent TERM: %v", err)
	}
	key(uv.KeyEscape)
	if m.runawayOpen.id != "" {
		t.Fatal("Escape did not dismiss narrow dialog")
	}
	m.size = wide
	h.paintNow()
	choose("hold")
	click(m.regions.DialogActions[2])
	h.until("focus remote pane", func() bool {
		return m.active == "remote" && m.endpoints["remote"].snapshot.State.FocusedPane() == 2 && m.runawayPainted.id == ""
	})
	choose("hold")
	click(m.regions.DialogActions[0])
	h.until("SIGTERM reached uncooperative process", func() bool {
		_, err := os.Stat(filepath.Join(dir, "hold"))
		return err == nil
	})
	if m.runawayPainted.id == "" {
		t.Fatal("dialog closed while process still alive")
	}
	key('2')
	choose("term")
	click(m.regions.DialogActions[0])
	choose("snooze")
	snoozed, _ := readAlert("snooze")
	click(m.regions.DialogActions[3])
	h.until("terminated alerts and snoozed alert disappear", func() bool {
		return len(m.endpoints["remote"].runawayAlerts) == 0 && m.runawayToast.id == ""
	})
	if _, err := os.Stat(filepath.Join(dir, "term")); err != nil {
		t.Fatalf("SIGTERM was not handled by exiting fixture: %v", err)
	}
	if err := syscall.Kill(snoozed.PID, 0); err != nil {
		t.Fatalf("snoozed process should still be alive: %v", err)
	}
}

func TestRunawayUnresolvedPathDrawsReasonWithoutStopButtons(t *testing.T) {
	t.Parallel()
	a := endpoint.RunawayAlert{Machine: "host", Pane: 2, PID: 100, Name: "sleep", RSS: 2048,
		StopDisabledReason: "実行ファイルを照合できないため停止できません。ペインへ移動して確認してください"}
	out := render.ComposeClient(render.Input{State: model.New(t.TempDir(), 100, 25), Dialog: runawayDialog(a)}, nil, "", false)
	var displayed strings.Builder
	for _, cell := range out.Frame.Cells {
		displayed.WriteString(cell.Content)
	}
	if !strings.Contains(displayed.String(), a.StopDisabledReason) || strings.Contains(displayed.String(), "SIGKILL") ||
		len(out.DialogActions) != 4 || out.DialogActions[0].W != 0 || out.DialogActions[1].W != 0 ||
		out.DialogActions[2].W == 0 || out.DialogActions[3].W == 0 {
		t.Fatalf("unavailable stop dialog: bounds %+v", out.DialogActions)
	}
}

func TestRunawayFocusKeepsDialogBehindLocalAndRemoteModalPanes(t *testing.T) {
	h := newRightMachineHarnessConfigured(t, func(string) string {
		return "\n[runaway]\nenabled=true\npoll_interval_seconds=2\nmemory_mb=40\n"
	}, true)
	m := h.m
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, id := range []string{"local", "remote"} {
		command := machine.Command([]string{"env", "IKESU_TEST_RUNAWAY_MEMORY=" + filepath.Join(dir, id), exe, "-test.run=^TestRunawayMemoryFixture$"})
		h.api(id, api.MethodPaneSendText, api.PaneSendTextParams{PaneID: "p2", Text: command + "\n"})
	}
	h.until("local and remote warnings", func() bool {
		return len(m.endpoints["local"].runawayAlerts) == 1 && len(m.endpoints["remote"].runawayAlerts) == 1
	})
	for _, id := range []string{"local", "remote"} {
		for _, modal := range []string{"overlay", "popup"} {
			h.api(id, "pane.command.execute", api.PaneCommandExecuteParams{PaneID: "p1", Action: modal, Command: "exec /bin/cat"})
			st := m.endpoints[id]
			h.until(id+" modal took input", func() bool {
				return st.snapshot.State.OverlayVisible() || st.snapshot.State.PopupVisible()
			})
			before := st.snapshot.State.FocusedManagedPane()
			warning := st.runawayAlerts[0]
			h.paintNow()
			// Inspect the same warning that the toast or “next warning” selects.
			m.inspectRunaway(runawaySelection{id, warning.ID})
			h.until("dialog opened", func() bool { return m.runawayPainted == (runawaySelection{id, warning.ID}) })
			if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: '3'}}); err != nil {
				t.Fatal(err)
			}
			h.until("focus rejected immediately", func() bool { return m.runawayFocusError != "" })
			var view render.Input
			m.overlayRunaway(&view)
			if !strings.Contains(m.runawayFocusError, "別のポップアップ") || view.Dialog == nil ||
				!strings.Contains(view.Dialog.Detail, m.runawayFocusError) || m.pending != nil ||
				m.active != "local" || st.snapshot.State.FocusedManagedPane() != before || m.runawayPainted.id != warning.ID {
				t.Fatalf("%s/%s: focus %d, active %s, pending %+v, dialog %+v, reason %q",
					id, modal, st.snapshot.State.FocusedManagedPane(), m.active, m.pending, view.Dialog, m.runawayFocusError)
			}
			if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEscape}}); err != nil {
				t.Fatal(err)
			}
			if modal == "popup" {
				h.api(id, "popup.close", api.EmptyParams{})
			} else {
				h.api(id, api.MethodPaneClose, api.PaneTarget{PaneID: fmt.Sprintf("p%d", before)})
			}
			h.until("modal explicitly closed", func() bool {
				return !st.snapshot.State.OverlayVisible() && !st.snapshot.State.PopupVisible()
			})
		}
	}
}
