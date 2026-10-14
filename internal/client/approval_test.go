package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineApprovalExpiryStopsRetryAndConnectionClearsSharedRecord(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GTX_STATE_DIR", root)
	// Hold a real pre-existing shared record so event processing cannot launch a browser.
	dir := filepath.Join(root, "machine-approvals")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "saved-id"
	record := filepath.Join(dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(id))))
	if err := os.WriteFile(record, []byte(`{"client":"other","url":"old","opened_at":"2026-10-14T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(root, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	snapshot := render.Input{State: model.New(root, 100, 40)}
	e := &managedEndpoint{profile: machine.Profile{ID: id, Target: "changed-target", Enabled: true}, attempt: 1}
	m := &machineClient{ctx: context.Background(), out: file, active: "local", size: tui.Winsize{Cols: 100, Rows: 40}, order: []string{"local", id}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot}, id: e}}
	m.event(endpointEvent{id: id, attempt: 1, approval: "https://example.invalid/new"})
	if e.status != "awaiting approval" || e.approval != "https://example.invalid/new" {
		t.Fatal(e.status, e.approval)
	}
	m.event(endpointEvent{id: id, attempt: 1, err: fmt.Errorf("SSH approval expired: wait ended")})
	if e.status != machine.ApprovalStopped || !e.retry.manual || !e.retry.next.IsZero() {
		t.Fatal(e.status, e.retry)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatal("expiry lost record", err)
	}
	m.event(endpointEvent{id: id, attempt: 1, connection: &endpointConnection{}})
	if e.status != "online" || e.retry.manual {
		t.Fatal(e.status, e.retry)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("connection kept record", err)
	}
}

func TestMachineApprovalHeaderClickCancelsWaitAndStartsNewAttempt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("GTX_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("GTX_CONFIG", filepath.Join(root, "config.toml"))
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	id := "saved-id"
	record := filepath.Join(root, "state", "machine-approvals", fmt.Sprintf("%x.json", sha256.Sum256([]byte(id))))
	if err := os.MkdirAll(filepath.Dir(record), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"client":"prior","url":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(root, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	snapshot := render.Input{State: model.New(root, 160, 45)}
	waiting, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remote := &managedEndpoint{profile: machine.Profile{ID: id, Label: "Approval", Target: "127.0.0.1", Enabled: true}, status: machine.ApprovalStopped, approval: "old-url", cancel: cancelWait, attempt: 1}
	remote.retry.approvalExpired()
	m := &machineClient{ctx: ctx, settings: &localSettings{}, events: make(chan endpointEvent), out: file, active: "local", size: tui.Winsize{Cols: 160, Rows: 45}, order: []string{"local", id}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot}, id: remote}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	var header *render.MachineHit
	for _, hit := range m.sidebar.Hits {
		if hit.Machine == id && hit.Header {
			copy := hit
			header = &copy
			break
		}
	}
	if header == nil {
		t.Fatal("header absent")
	}
	event := input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: header.Rect.X, Y: header.Rect.Y, Button: uv.MouseLeft}}
	if err := m.input(event); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting.Done():
	default:
		t.Fatal("previous wait was not canceled")
	}
	if remote.attempt != 2 || remote.approval != "" || remote.retry.manual {
		t.Fatal(remote.attempt, remote.approval, remote.retry)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("explicit retry did not clear record", err)
	}
	// The new worker uses real OpenSSH with isolated generated config. Its parent
	// is already canceled, so it closes that owned child without external state.
	m.workers.Wait()
}
