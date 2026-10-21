package client

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/input"
)

func TestMachineDragTimeoutDiscardsDropWithoutChangingLocal(t *testing.T) {
	start := time.Unix(1, 0)
	m := &machineClient{active: "local", endpoints: map[string]*managedEndpoint{"local": {}, "remote": {}}, pending: &pendingMachine{id: "remote", deadline: start.Add(machineSwitchWait)}, drag: &machineDrag{id: "remote", started: true, released: true, workspace: 7, drop: 2}}
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

func TestActiveMachineCardReleaseOnDifferentRowDoesNotOpenMenu(t *testing.T) {
	var framer input.Framer
	events := framer.Push([]byte("\x1b[<0;5;10m"))
	if len(events) != 1 || events[0].Kind != input.KindMouseRelease {
		t.Fatal(events)
	}
	m := &machineClient{drag: &machineDrag{id: "remote", workspace: 9, selected: true, y: 8, drop: -1}, suppressRelease: true}
	if err := m.dragInput(events[0]); err != nil || m.drag != nil || m.suppressRelease {
		t.Fatal("release outside pressed row retained gesture or opened menu", err)
	}
}
