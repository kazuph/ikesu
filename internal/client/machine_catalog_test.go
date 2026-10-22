package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/machine"
)

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
