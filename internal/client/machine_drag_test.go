package client

import (
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/input"
)

func TestMachineDragTimeoutDiscardsDropWithoutChangingLocal(t *testing.T) {
	start := time.Unix(1, 0)
	m := &machineClient{active: "local", endpoints: map[string]*managedEndpoint{"local": {}, "remote": {}}, pending: &pendingMachine{id: "remote", deadline: start.Add(machineSwitchWait)}, drag: &machineDrag{id: "remote", started: true, released: true, queued: [][]byte{[]byte("drop")}}}
	m.advance(start.Add(machineSwitchWait - time.Nanosecond))
	if m.drag == nil || m.pending == nil || m.notice != "" {
		t.Fatal("drag expired before the switch deadline")
	}
	m.advance(start.Add(machineSwitchWait))
	if m.drag != nil || m.pending != nil || m.active != "local" || m.notice != "the space could not be moved: its machine is not ready" {
		t.Fatal("expired drag was retained or changed the Local view")
	}
}

func TestMachineDragReleaseWithoutValidTargetDoesNotFailClient(t *testing.T) {
	m := &machineClient{drag: &machineDrag{id: "remote", started: true}}
	if err := m.dragInput(input.Event{Kind: input.KindMouseRelease}); err != nil || m.drag != nil {
		t.Fatal("invalid target release failed the client or retained the gesture", err)
	}
}
