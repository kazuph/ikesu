package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineConnectionButtonsStartAndStopAttempt(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(configPath, []byte("["), 0600); err != nil {
		t.Fatal(err)
	}
	// Stop the real connection worker at config loading, before it starts SSH.
	t.Setenv(config.EnvVar, configPath)
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m := &machineClient{session: "catalog-test", active: "local", out: out, ctx: ctx, events: make(chan endpointEvent, 1),
		settings: &localSettings{}, size: tui.Winsize{Cols: 120, Rows: 45}, endpoints: map[string]*managedEndpoint{
			"local": {profile: machine.Profile{ID: "local", Label: "Local", Enabled: true}, snapshot: &render.Input{State: model.New(t.TempDir(), 120, 45)}},
		}}
	defer func() { cancel(); m.workers.Wait() }()
	catalog := machine.Catalog{Path: m.catalogPath()}
	profile, err := catalog.AddVerified("host", "Remote", "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Change(profile.ID, "disable", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshMachineCatalog(); err != nil {
		t.Fatal(err)
	}
	remote := m.endpoints[profile.ID]
	button := func() render.MachineHit {
		t.Helper()
		var target render.MachineHit
		for _, hit := range m.sidebar.Hits {
			if hit.Machine == profile.ID && hit.IsHeaderButton() {
				if target.Rect.W != 0 {
					t.Fatalf("machine has multiple buttons: %+v and %+v", target, hit)
				}
				target = hit
			}
		}
		if target.Rect.W == 0 {
			t.Fatal("missing machine header button")
		}
		return target
	}
	connect := button()
	drawn, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !connect.Connect || !strings.Contains(ansi.Strip(string(drawn)), "[connect]") {
		t.Fatal("missing disconnected machine heading or connect button")
	}
	click := func(x, y int) {
		t.Helper()
		for _, suffix := range []string{"M", "m"} {
			if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%d%s", x+1, y+1, suffix))); err != nil {
				t.Fatal(err)
			}
		}
	}
	click(0, connect.Rect.Y)
	if !m.sidebar.Collapsed[profile.ID] || remote.profile.Enabled || remote.attempt != 0 {
		t.Fatal("disconnected heading click did not just collapse the group")
	}
	remote.retry = retryPolicy{delay: remoteRetryLimit, next: time.Now().Add(remoteRetryLimit)}
	click(connect.Rect.X+connect.Rect.W-1, connect.Rect.Y)
	if !catalog.Load()[0].Enabled || !remote.profile.Enabled || remote.status != "connecting" || remote.attempt != 1 || remote.retry.delay != 0 || !remote.retry.next.IsZero() || m.pending != nil {
		t.Fatalf("connect did not start a fresh background attempt: %+v", remote)
	}
	attemptContext := remote.ctx
	remote.retry = retryPolicy{delay: initialRetryDelay, next: time.Now()}
	disconnect := button()
	if !disconnect.Disconnect {
		t.Fatalf("connecting machine has wrong button: %+v", disconnect)
	}
	click(disconnect.Rect.X+disconnect.Rect.W-1, disconnect.Rect.Y)
	if saved := catalog.Load(); len(saved) != 1 || saved[0].Enabled {
		t.Fatalf("disconnect was not saved: %+v", saved)
	}
	m.advance(time.Now())
	if remote.profile.Enabled || remote.status != "disconnected" || remote.attempt != 2 ||
		attemptContext.Err() == nil || remote.retry != (retryPolicy{}) {
		t.Fatalf("disconnect did not stop the attempt and retries: %+v", remote)
	}
	if after := button(); !after.Connect {
		t.Fatalf("disconnected machine has wrong button: %+v", after)
	}
}

func TestMachineCatalogDisconnectReturnsToLocalAndStopsRetry(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now()
	peer := endpoint.Current()
	local := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}}
	m := &machineClient{session: "catalog-test", endpoints: map[string]*managedEndpoint{
		"local": {connection: local},
	}}
	catalog := machine.Catalog{Path: m.catalogPath()}
	profile, err := catalog.AddVerified("host", "Remote", "default")
	if err != nil {
		t.Fatal(err)
	}
	m.active = profile.ID
	m.pending = &pendingMachine{id: profile.ID, keys: []input.Event{{Kind: input.KindKey}}}
	remote := &managedEndpoint{profile: profile, cancel: cancel, attempt: 1, retry: retryPolicy{next: now, delay: initialRetryDelay}}
	m.endpoints[profile.ID] = remote
	if _, err := catalog.Change(profile.ID, "disable", "", nil); err != nil {
		t.Fatal(err)
	}
	m.event(endpointEvent{catalogChanged: true})
	m.event(endpointEvent{id: profile.ID, attempt: 1, err: errors.New("late SSH error")})
	m.advance(now)
	if m.active != "local" || m.pending != nil || ctx.Err() == nil || remote.connection != nil || remote.retry != (retryPolicy{}) || remote.attempt != 2 {
		t.Fatalf("disconnect left active=%s pending=%+v cancelled=%v remote=%+v", m.active, m.pending, ctx.Err(), remote)
	}
	if remote.profile.Enabled || remote.status != "disconnected" || remote.reason != "machine is disconnected" {
		t.Fatalf("unexpected disconnected state: %+v", remote)
	}
	if len(local.queue) != 2 || local.queue[0].Type != "resize" || local.queue[1].Type != "visibility" || local.queue[1].Visible == nil || !*local.queue[1].Visible {
		t.Fatalf("Local viewer was not restored: %+v", local.queue)
	}
}

func TestLocalReconnectThatStartedHiddenRestoresVisibleViewer(t *testing.T) {
	t.Parallel()
	peer := endpoint.Current()
	local := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}}
	m := &machineClient{active: "remote", order: []string{"local"}, settings: &localSettings{}, endpoints: map[string]*managedEndpoint{"local": {}}}
	m.showLocalAfterDisconnect()
	m.event(endpointEvent{id: "local", connection: local, hidden: true})
	if len(local.queue) != 2 || local.queue[0].Type != "resize" || local.queue[1].Type != "visibility" || local.queue[1].Visible == nil || !*local.queue[1].Visible {
		t.Fatalf("Local viewer stayed hidden after reconnect: %+v", local.queue)
	}
}

func TestMachineCatalogRemovalCancelsPendingSwitchAndOwnedAttempt(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote := &managedEndpoint{profile: machine.Profile{ID: "remote", Enabled: true}, cancel: cancel}
	m := &machineClient{session: "catalog-test", active: "local", endpoints: map[string]*managedEndpoint{"local": {}, "remote": remote}, pending: &pendingMachine{id: "remote", deadline: time.Now()}}
	if err := os.MkdirAll(filepath.Dir(m.catalogPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.catalogPath(), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshMachineCatalog(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || m.pending != nil || m.active != "local" || m.endpoints["remote"] != nil {
		t.Fatal("removed endpoint retained a switch/attempt or changed Local selection")
	}
	m.advance(time.Now())
}

func TestMachineCatalogInvalidJSONKeepsConnectionsAndSelection(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote := &managedEndpoint{profile: machine.Profile{ID: "remote", Enabled: true}, cancel: cancel}
	m := &machineClient{session: "catalog-test", active: "remote", order: []string{"local", "remote"}, endpoints: map[string]*managedEndpoint{"local": {}, "remote": remote}}
	if err := os.MkdirAll(filepath.Dir(m.catalogPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.catalogPath(), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.refreshMachineCatalog(); err == nil {
		t.Fatal("invalid catalog silently replaced saved machines")
	}
	if ctx.Err() != nil || m.active != "remote" || m.endpoints["remote"] != remote || len(m.order) != 2 {
		t.Fatal("invalid catalog changed selected endpoint or stopped its owned attempt")
	}
}
