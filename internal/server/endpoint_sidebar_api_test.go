package server

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/client"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestSSHRelayUsesExistingSidebarAPIsAndPreservesSelection(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	var original api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, nil, &original)
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: strptr(t.TempDir())}, &created)
	for _, request := range []api.Request{
		{ID: "section", Method: api.MethodWorkspaceSetSection, Params: api.MarshalParams(api.WorkspaceSetSectionParams{WorkspaceID: created.Workspace.WorkspaceID, Section: "work"})},
		{ID: "move", Method: "workspace.move", Params: api.MarshalParams(map[string]any{"workspace_id": created.Workspace.WorkspaceID, "insert_index": 0})},
		{ID: "missing", Method: "workspace.move", Params: api.MarshalParams(map[string]any{"workspace_id": "s99999", "insert_index": 0})},
	} {
		input, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		read, output, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { finished <- client.Relay("test", input, output) }()
		if err := api.WriteRequest(write, request); err != nil {
			t.Fatal(err)
		}
		read.SetReadDeadline(time.Now().Add(5 * time.Second))
		response, err := api.ReadResponse(bufio.NewReader(read))
		if err != nil || response.ID != request.ID {
			t.Fatal("API response changed", response, err)
		}
		if request.ID == "missing" {
			if response.Err == nil || response.Err.Code != api.ErrWorkspaceNotFound {
				t.Fatal("missing original ID accepted", response)
			}
		} else if response.Err != nil {
			t.Fatal(response.Err)
		}
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("API relay waited for SSH stdin EOF")
		}
		input.Close()
		write.Close()
		read.Close()
		output.Close()
	}
	var final api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, nil, &final)
	if final.Workspaces[0].WorkspaceID != created.Workspace.WorkspaceID {
		t.Fatal("public API mutations were not applied", final)
	}
	viewer := dialClient(t, dir)
	defer viewer.conn.Close()
	viewer.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	viewer.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var decoder endpoint.SnapshotDecoder
	for {
		message, err := readClientMsg(viewer.rd)
		if err != nil {
			t.Fatal(err)
		}
		if message.Type != "snapshot" {
			continue
		}
		data, err := decode64(message.Data)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State.Workspaces[0].SidebarSection() != "work" {
			t.Fatal("API section change missing from actual snapshot")
		}
		break
	}
	for _, ws := range final.Workspaces {
		if ws.WorkspaceID == original.Workspaces[0].WorkspaceID && !ws.Focused {
			t.Fatal("sidebar mutation changed canonical selection")
		}
	}
}
