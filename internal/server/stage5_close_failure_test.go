package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestStage5WorkspaceCloseFailureReturnsErrorAndKeepsOriginal(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)
	var before api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &before)
	old := firstPaneID(t, dir)
	home := os.Getenv("HOME")
	teststate.Setenv(t, "HOME", filepath.Join(t.TempDir(), "missing-home"))
	reply := apiReq(t, dir, api.MethodWorkspaceClose, api.WorkspaceTarget{WorkspaceID: before.Workspaces[0].WorkspaceID})
	if reply.Err == nil || reply.Err.Message == "" {
		t.Fatalf("cancelled close returned success: %+v", reply)
	}
	var after api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &after)
	if len(after.Workspaces) != 1 || after.Workspaces[0].WorkspaceID != before.Workspaces[0].WorkspaceID || firstPaneID(t, dir) != old {
		t.Fatal("failed close changed original identity", after)
	}
	teststate.Setenv(t, "HOME", home)
	apiOK(t, dir, api.MethodWorkspaceClose, api.WorkspaceTarget{WorkspaceID: before.Workspaces[0].WorkspaceID}, nil)
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &after)
	if len(after.Workspaces) != 1 || after.Workspaces[0].WorkspaceID == before.Workspaces[0].WorkspaceID || firstPaneID(t, dir) == old {
		t.Fatal("recovered close did not create a new home", after)
	}
}
