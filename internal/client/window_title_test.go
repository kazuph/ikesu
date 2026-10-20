package client

import (
	"bytes"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestWindowTitleMatchesWorkspaceAndTabActivationBytes(t *testing.T) {
	state := model.New("/tmp", 160, 45)
	var title windowTitleState
	var output bytes.Buffer
	paint := func(want string) {
		t.Helper()
		output.Reset()
		for i := 0; i < 3; i++ {
			if err := title.painted(&output, render.Input{State: state}); err != nil {
				t.Fatal(err)
			}
		}
		if output.String() != want {
			t.Fatalf("title bytes got %q want %q", output.String(), want)
		}
	}
	two := "\x1b]0;ikesu\x07\x1b]0;ikesu\x07"
	paint(two)
	state.Apply(model.CmdNewWorkspace{Cwd: "/tmp"})
	paint("")
	state.Active = 0
	paint("")
	state.Apply(model.CmdNewTab{})
	paint("")
	state.ActiveWorkspace().ActiveTab = 0
	paint(two)
}

func TestTitleGroupsExcludeDisconnectedStaleAndDisabledSnapshots(t *testing.T) {
	state := model.New("/tmp", 160, 45)
	snapshot := &render.Input{State: state}
	endpoint := &managedEndpoint{snapshot: snapshot, connection: &endpointConnection{}, attempt: 2, snapshotAttempt: 2}
	endpoint.profile.Enabled = true
	m := &machineClient{order: []string{"local"}, endpoints: map[string]*managedEndpoint{"local": endpoint}}
	if m.titleGroups()[0].Input != snapshot {
		t.Fatal("current snapshot omitted")
	}
	endpoint.snapshotAttempt = 1
	if m.titleGroups()[0].Input != nil {
		t.Fatal("old connection counted")
	}
	endpoint.snapshotAttempt = 2
	endpoint.connection = nil
	if m.titleGroups()[0].Input != nil {
		t.Fatal("disconnected snapshot counted")
	}
	endpoint.connection = &endpointConnection{}
	endpoint.profile.Enabled = false
	if m.titleGroups()[0].Input != nil {
		t.Fatal("disabled snapshot counted")
	}
}
