package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointPresentationUpdateIsViewerLocalAndRejectsInvalidPreferences(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	first := dialClient(t, dir)
	defer first.conn.Close()
	second := dialClient(t, dir)
	defer second.conn.Close()
	type received struct {
		in  render.Input
		err error
	}
	readers := make(map[*testConn]chan received)
	for _, c := range []*testConn{first, second} {
		updates := make(chan received, 1)
		readers[c] = updates
		done := make(chan struct{})
		go func(c *testConn) {
			defer close(done)
			var decoder endpoint.SnapshotDecoder
			for {
				message, err := readClientMsg(c.rd)
				if err != nil {
					return
				}
				value := received{}
				switch message.Type {
				case "snapshot":
					data, err := decode64(message.Data)
					if err == nil {
						value.in, err = decoder.Decode(data)
					}
					value.err = err
				case "error":
					value.err = fmt.Errorf("%s", message.Reason)
				default:
					continue
				}
				select {
				case <-updates:
				default:
				}
				updates <- value
			}
		}(c)
		t.Cleanup(func() { c.conn.Close(); <-done })
		c.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
	}
	readSnapshot := func(c *testConn, matches func(render.Input) bool) render.Input {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case value := <-readers[c]:
				if value.err != nil {
					t.Fatal(value.err)
				}
				if matches(value.in) {
					return value.in
				}
			case <-deadline:
				t.Fatal("viewer snapshot did not arrive")
			}
		}
	}
	initial := readSnapshot(first, func(render.Input) bool { return true })
	readSnapshot(second, func(render.Input) bool { return true })
	split := .6
	first.send(t, clientMsg{Type: "view_config", Preferences: &endpoint.ViewPreferences{SidebarWidth: 34, SidebarManual: true, SidebarSectionSplit: &split}})
	updated := readSnapshot(first, func(in render.Input) bool { return in.State.SidebarSize == 34 })
	if !updated.State.SidebarManual || updated.State.SidebarSectionSplit == nil || *updated.State.SidebarSectionSplit != split {
		t.Fatal("presentation update lost the saved preferences", updated.State)
	}
	// A new snapshot from the second viewer must retain its own display state.
	second.send(t, clientMsg{Type: "resize", Cols: 159, Rows: 45})
	other := readSnapshot(second, func(in render.Input) bool { return in.State.Cols == 159 })
	if other.State.SidebarSize != initial.State.SidebarSize || other.State.SidebarSectionSplit != nil {
		t.Fatal("updating one viewer changed another", other.State)
	}
	first.send(t, clientMsg{Type: "view_config", Preferences: &endpoint.ViewPreferences{SidebarWidth: 0}})
	deadline := time.After(5 * time.Second)
	for rejected := false; !rejected; {
		select {
		case value := <-readers[first]:
			if value.err != nil {
				if value.err.Error() != "endpoint_preferences_invalid" {
					t.Fatal(value.err)
				}
				rejected = true
			}
		case <-deadline:
			t.Fatal("invalid preferences were not rejected")
		}
	}
	first.send(t, clientMsg{Type: "resize", Cols: 158, Rows: 45})
	after := readSnapshot(first, func(in render.Input) bool { return in.State.Cols == 158 })
	if after.State.SidebarSize != 34 {
		t.Fatal("invalid preferences changed the viewer", after.State)
	}
}

func TestEndpointPresentationUpdateRequiresGenerationTwo(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	c := dialClient(t, dir)
	defer c.conn.Close()
	peer := endpoint.Current()
	peer.Generation = 1
	c.send(t, clientMsg{Type: "hello", Peer: peer, Cols: 80, Rows: 24})
	c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "welcome" })
	c.send(t, clientMsg{Type: "view_config", Preferences: &endpoint.ViewPreferences{SidebarWidth: 34}})
	messages := c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "error" })
	if messages[len(messages)-1].Reason != "endpoint_action_unsupported: view_config" {
		t.Fatal(messages)
	}
}
