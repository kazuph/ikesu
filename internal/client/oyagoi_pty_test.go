package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/tui"
)

// Starts two real servers and verifies the destination PTY after reconnection.
func TestOyagoiPendingInputNeverReachesReusedWorkspaceActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	old := e.snapshot.State.Workspaces[0]
	replacement := m.endpoints["local"].snapshot.State.Workspaces[0]
	if old.ID != replacement.ID || old.Generation == replacement.Generation {
		t.Fatal("fixture must reuse number with different generation")
	}
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", old.ID, old.Generation)); err != nil {
		t.Fatal(err)
	}
	if err := m.selectOyagoi(); err != nil {
		t.Fatal(err)
	}
	if !m.holdPendingInput(input.Event{Kind: input.KindKey, Raw: []byte("printf HQ_FORBIDDEN_INPUT\\n\n")}, time.Now()) {
		t.Fatal("input was not held")
	}
	if err := e.connection.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", stream)
	attempt := e.attempt
	h.until("replacement server snapshot", func() bool { return e.connection != nil && e.snapshotAttempt == attempt })
	if m.active != "local" {
		t.Fatal("viewer switched to replacement")
	}
	if err := m.sendInput("remote", []byte("printf HQ_BARRIER_OK\\n\n")); err != nil {
		t.Fatal(err)
	}
	var text string
	h.until("replacement PTY barrier", func() bool {
		conn, err := net.DialTimeout("unix", api.APISocketPath(h.dirs["local"]), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := api.WriteRequest(conn, api.Request{ID: "read", Method: api.MethodPaneRead, Params: api.MarshalParams(api.PaneReadParams{PaneID: "p1", Source: "recent", StripANSI: true})}); err != nil {
			t.Fatal(err)
		}
		response, err := api.ReadResponse(bufio.NewReader(conn))
		if err != nil || response.Err != nil {
			t.Fatalf("read: %+v %v", response, err)
		}
		var result api.PaneReadResultEnvelope
		if err := json.Unmarshal(response.Result, &result); err != nil {
			t.Fatal(err)
		}
		text = result.Read.Text
		return strings.Contains(text, "HQ_BARRIER_OK")
	})
	if strings.Contains(text, "HQ_FORBIDDEN_INPUT") {
		t.Fatalf("held input reached replacement PTY: %s", text)
	}
}

// Starts two real servers and waits for a reconnected endpoint, which can exceed five seconds.
func TestOyagoiRejectsReusedNumberBeforeNewSnapshotActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	old := e.snapshot.State.Workspaces[0]
	other := m.endpoints["local"].snapshot.State.Workspaces[0]
	if old.ID != other.ID || old.Generation == other.Generation {
		t.Fatal("fixture must reuse number with different generation")
	}
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", old.ID, old.Generation)); err != nil {
		t.Fatal(err)
	}
	if err := e.connection.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", stream)
	attempt := e.attempt
	h.until("new connection before snapshot", func() bool { return e.connection != nil })
	if e.snapshotAttempt == attempt {
		t.Fatal("fixture consumed new snapshot too early")
	}
	if err := m.selectOyagoi(); err == nil || m.pending != nil || m.active != "local" {
		t.Fatalf("stale HQ selected: %v pending=%+v", err, m.pending)
	}
	got, err := m.oyagoiStore.load()
	if err != nil || got == nil || got.Generation != old.Generation {
		t.Fatalf("saved identity changed: %+v %v", got, err)
	}
}

// Real server disconnection, a local menu and two durable-store updates may exceed five seconds.
func TestOyagoiOfflineUnsetKeepsNewerSelectionActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.forms = tui.NewMachineForms(m.ctx, m.catalogPath(), func(fn func()) { fn() })
	defer m.forms.Close()
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	remote := m.endpoints["remote"]
	ws := remote.snapshot.State.Workspaces[0]
	old := oyagoiSelection{Machine: "remote", Session: m.selectionSession("remote"), Workspace: ws.ID, Generation: ws.Generation}
	if err := m.oyagoiStore.set(&old); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshOyagoi(); err != nil {
		t.Fatal(err)
	}
	if err := remote.connection.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["remote"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", stream)
	attempt := remote.attempt
	h.until("connected managed endpoint", func() bool { return remote.connection != nil && remote.snapshotAttempt == attempt })
	if err := remote.connection.Close(); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for remote.connection != nil {
		select {
		case ev := <-m.events:
			m.event(ev)
		case <-timer.C:
			t.Fatal("disconnect timeout")
		}
	}
	open := func() {
		t.Helper()
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		for _, hit := range m.sidebar.Hits {
			if hit.OfflineOyagoi {
				if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<2;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatal("offline HQ has no action")
	}
	open()
	newer := old
	newer.Generation = "newer"
	if err := m.oyagoiStore.set(&newer); err != nil {
		t.Fatal(err)
	}
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.oyagoiStore.load(); err != nil || got == nil || *got != newer {
		t.Fatalf("stale unset erased newer: %+v %v", got, err)
	}
	if err := m.oyagoiStore.set(&old); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshOyagoi(); err != nil {
		t.Fatal(err)
	}
	open()
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.oyagoiStore.load(); err != nil || got != nil {
		t.Fatalf("offline unset failed: %+v %v", got, err)
	}
}

func TestOyagoiCatalogUpdatesProjectionActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	ws := e.snapshot.State.Workspaces[0]
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
		t.Fatal(err)
	}
	h.until("initial HQ projection", func() bool { return m.endpoints["local"].snapshot.MobileForeignOyagoi != "" })
	writeCatalog := func(profile machine.Profile) {
		t.Helper()
		data, err := json.Marshal([]machine.Profile{profile})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(m.catalogPath()), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(m.catalogPath(), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := m.refreshMachineCatalog(); err != nil {
			t.Fatal(err)
		}
	}
	profile := e.profile
	profile.Label = "renamed-review-machine"
	writeCatalog(profile)
	h.until("renamed projection", func() bool {
		return strings.Contains(m.endpoints["local"].snapshot.MobileForeignOyagoi, "renamed-review-machine")
	})
	profile.Enabled = false
	writeCatalog(profile)
	h.until("offline projection", func() bool { return strings.Contains(m.endpoints["local"].snapshot.MobileForeignOyagoi, "(offline)") })
	if err := os.WriteFile(m.catalogPath(), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshMachineCatalog(); err != nil {
		t.Fatal(err)
	}
	h.until("removed projection", func() bool {
		label := m.endpoints["local"].snapshot.MobileForeignOyagoi
		return m.endpoints["remote"] == nil && strings.Contains(label, "remote") && strings.Contains(label, "(offline)")
	})
}

func TestOyagoiTwoServersActualPTYProjectionAndReplacement(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	remote := m.endpoints["remote"].snapshot.State.Workspaces[0]
	local := m.endpoints["local"].snapshot.State.Workspaces[0]
	remoteSet := fmt.Sprintf("oyagoi:set:%d:%s", remote.ID, remote.Generation)
	remoteUnset := fmt.Sprintf("oyagoi:unset:%d:%s", remote.ID, remote.Generation)
	localSet := fmt.Sprintf("oyagoi:set:%d:%s", local.ID, local.Generation)
	if err := m.oyagoiAction("remote", remoteSet); err != nil {
		t.Fatal(err)
	}
	h.until("remote HQ projected to both viewers", func() bool {
		return m.endpoints["remote"].snapshot.MobileOyagoi == remote.ID && m.endpoints["local"].snapshot.MobileForeignOyagoi != ""
	})
	if err := m.oyagoiAction("local", localSet); err != nil {
		t.Fatal(err)
	}
	if err := m.oyagoiAction("remote", remoteUnset); err != nil {
		t.Fatal(err)
	}
	h.until("replacement projected to both viewers", func() bool {
		return m.endpoints["local"].snapshot.MobileOyagoi == local.ID && m.endpoints["remote"].snapshot.MobileForeignOyagoi != ""
	})
	got, err := m.oyagoiStore.load()
	if err != nil || got == nil || got.Machine != "local" || got.Generation != local.Generation {
		t.Fatalf("selected HQ = %+v, %v", got, err)
	}
	h.api("local", api.MethodWorkspaceClose, map[string]any{"workspace_id": fmt.Sprintf("s%d", local.ID)})
	h.until("deleted HQ confirmed by its server", func() bool {
		got, err := m.oyagoiStore.load()
		return err == nil && got == nil && m.oyagoi == nil
	})
}

func TestOyagoiDisconnectAndReconnectActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	ws := e.snapshot.State.Workspaces[0]
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
		t.Fatal(err)
	}
	h.until("HQ projected", func() bool { return e.snapshot.MobileOyagoi == ws.ID })
	e.connection.Close()
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["remote"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", stream)
	attempt := e.attempt
	h.until("managed connection", func() bool { return e.connection != nil && e.snapshotAttempt == attempt })
	e.connection.Close()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for e.connection != nil {
		select {
		case event := <-m.events:
			m.event(event)
		case <-timer.C:
			t.Fatal("disconnection not observed")
		}
	}
	if got := m.oyagoiForSidebar(); got == nil || !got.Offline {
		t.Fatalf("disconnected HQ = %+v", got)
	}
	stream, err = net.Dial("unix", api.ClientSocketPath(h.dirs["remote"]))
	if err != nil {
		t.Fatal(err)
	}
	m.start("remote", stream)
	attempt = e.attempt
	h.until("HQ restored after reconnect", func() bool {
		return e.connection != nil && e.snapshotAttempt == attempt && e.snapshot.MobileOyagoi == ws.ID && !m.oyagoiForSidebar().Offline
	})
	got, err := m.oyagoiStore.load()
	if err != nil || got == nil || got.Generation != ws.Generation {
		t.Fatalf("selection after reconnect = %+v, %v", got, err)
	}
}

func TestOyagoiMachineRenameAndSessionIdentityActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	ws := e.snapshot.State.Workspaces[0]
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
		t.Fatal(err)
	}
	h.until("HQ projected", func() bool { return e.snapshot.MobileOyagoi == ws.ID })
	e.profile.Label = "renamed machine"
	if got := m.oyagoiForSidebar(); got == nil || got.Workspace != ws.ID || got.Offline {
		t.Fatalf("HQ after machine rename = %+v", got)
	}
	selection, err := m.oyagoiStore.load()
	if err != nil || selection == nil || selection.Machine != "remote" || selection.Session != "right-remote" {
		t.Fatalf("selection after machine rename = %+v, %v", selection, err)
	}
	e.profile.Session = "another-session"
	if got := m.oyagoiForSidebar(); got == nil || !got.Offline {
		t.Fatalf("HQ shown on another session = %+v", got)
	}
	e.profile.Session = "right-remote"
	if got := m.oyagoiForSidebar(); got == nil || got.Offline || got.Workspace != ws.ID {
		t.Fatalf("HQ after returning to original session = %+v", got)
	}
}

func TestOyagoiDoesNotFollowSameMachineOtherSessionActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	m.oyagoiStore.path = filepath.Join(t.TempDir(), "oyagoi.json")
	e := m.endpoints["remote"]
	ws := e.snapshot.State.Workspaces[0]
	if ws.ID != m.endpoints["local"].snapshot.State.Workspaces[0].ID {
		t.Fatal("test requires matching workspace numbers")
	}
	if err := m.oyagoiAction("remote", fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
		t.Fatal(err)
	}
	h.until("original HQ projected", func() bool { return e.snapshot.MobileOyagoi == ws.ID })
	if err := e.connection.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	e.profile.Session = "right-local"
	m.start("remote", stream)
	attempt := e.attempt
	h.until("other session connected", func() bool { return e.connection != nil && e.snapshotAttempt == attempt })
	if got := m.oyagoiForSidebar(); got == nil || !got.Offline {
		t.Fatalf("HQ followed another session: %+v", got)
	}
	selected, err := m.oyagoiStore.load()
	if err != nil || selected == nil || selected.Session != "right-remote" || selected.Generation != ws.Generation {
		t.Fatalf("original HQ changed: %+v, %v", selected, err)
	}
}

func TestOyagoiTwoViewersShareExplicitSelectionsActualPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	first := h.m
	path := filepath.Join(t.TempDir(), "oyagoi.json")
	first.oyagoiStore.path = path
	output, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	second := &machineClient{ctx: first.ctx, session: first.session, active: "local", order: []string{"local"}, out: output, settings: first.settings, size: first.size, events: make(chan endpointEvent, 128), endpoints: map[string]*managedEndpoint{}, oyagoiStore: oyagoiStore{path}}
	stream, err := net.Dial("unix", api.ClientSocketPath(h.dirs["local"]))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := connectEndpoint(stream, false, second.size, second.settings, "", nil)
	if err != nil {
		stream.Close()
		t.Fatal(err)
	}
	defer stream.Close()
	second.endpoints["local"] = &managedEndpoint{profile: machine.Profile{ID: "local", Label: "Local", Session: first.session, Enabled: true}, connection: connection, ctx: first.ctx, status: "online"}
	go connection.writeQueued(first.ctx)
	go func() {
		for {
			message, err := connection.read()
			if err != nil {
				return
			}
			select {
			case second.events <- endpointEvent{id: "local", message: message}:
			case <-first.ctx.Done():
				return
			}
		}
	}()
	initial := first.endpoints["local"].snapshot.State.Workspaces[0]
	replacement := first.endpoints["local"].snapshot.State.Workspaces[1]
	old := oyagoiSelection{Machine: "local", Session: first.session, Workspace: initial.ID, Generation: initial.Generation}
	newer := oyagoiSelection{Machine: "local", Session: first.session, Workspace: replacement.ID, Generation: replacement.Generation}
	if err := first.oyagoiStore.set(&old); err != nil {
		t.Fatal(err)
	}
	if err := first.refreshOyagoi(); err != nil {
		t.Fatal(err)
	}
	if err := second.oyagoiStore.set(&newer); err != nil {
		t.Fatal(err)
	}
	if err := second.refreshOyagoi(); err != nil {
		t.Fatal(err)
	}
	for _, client := range []*machineClient{first, second} {
		client := client
		stop, err := client.watchOyagoi(first.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
	}
	until := func(label string, condition func() bool) {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for !condition() {
			select {
			case event := <-first.events:
				first.event(event)
				_ = first.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")})
			case event := <-second.events:
				second.event(event)
				_ = second.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")})
			case <-timer.C:
				t.Fatalf("timeout %s first=%+v second=%+v", label, first.oyagoi, second.oyagoi)
			}
		}
	}
	until("second viewer connected", func() bool { return second.endpoints["local"].snapshot != nil })
	until("both viewers show selection written before watch", func() bool {
		return first.oyagoi != nil && *first.oyagoi == newer && second.oyagoi != nil && *second.oyagoi == newer && first.endpoints["local"].snapshot.MobileOyagoi == replacement.ID && second.endpoints["local"].snapshot.MobileOyagoi == replacement.ID
	})
	ws1 := first.endpoints["local"].snapshot.State.Workspaces[0]
	ws2 := second.endpoints["local"].snapshot.State.Workspaces[1]
	if err := first.oyagoiAction("local", fmt.Sprintf("oyagoi:set:%d:%s", ws1.ID, ws1.Generation)); err != nil {
		t.Fatal(err)
	}
	until("first choice seen by second", func() bool { return second.oyagoi != nil && second.oyagoi.Workspace == ws1.ID })
	if err := second.oyagoiAction("local", fmt.Sprintf("oyagoi:set:%d:%s", ws2.ID, ws2.Generation)); err != nil {
		t.Fatal(err)
	}
	until("replacement seen by first", func() bool { return first.oyagoi != nil && first.oyagoi.Workspace == ws2.ID })
	got, err := first.oyagoiStore.load()
	if err != nil || got == nil || got.Workspace != ws2.ID || got.Generation != ws2.Generation {
		t.Fatalf("shared HQ = %+v, %v", got, err)
	}
}

// Uses two real server viewers and a shared on-disk store.
func TestOyagoiRefreshAfterWatchRegistrationActualPTYReview(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	path := filepath.Join(t.TempDir(), "oyagoi.json")
	m.oyagoiStore.path = path
	first, second := m.endpoints["local"].snapshot.State.Workspaces[0], m.endpoints["local"].snapshot.State.Workspaces[1]
	old := oyagoiSelection{Machine: "local", Session: m.session, Workspace: first.ID, Generation: first.Generation}
	newer := oyagoiSelection{Machine: "local", Session: m.session, Workspace: second.ID, Generation: second.Generation}
	if err := m.oyagoiStore.set(&old); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshOyagoi(); err != nil {
		t.Fatal(err)
	}
	if err := (oyagoiStore{path}).set(&newer); err != nil {
		t.Fatal(err)
	}
	stop, err := m.watchOyagoi(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	h.until("newer HQ after watcher registration", func() bool {
		return m.oyagoi != nil && *m.oyagoi == newer && m.endpoints["local"].snapshot.MobileOyagoi == second.ID && m.endpoints["remote"].snapshot.MobileForeignOyagoi != ""
	})
}
