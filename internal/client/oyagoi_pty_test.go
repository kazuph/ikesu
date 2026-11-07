package client

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
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
