package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
)

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
	for _, client := range []*machineClient{first, second} {
		client := client
		stop, err := watchMachineCatalog(first.ctx, path, func(ctx context.Context, err error) { client.post(ctx, endpointEvent{oyagoiChanged: true, err: err}) })
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
