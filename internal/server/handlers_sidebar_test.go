package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"testing"
)

func TestG2DuplicateSectionAndMissingWorkspaceSocket(t *testing.T) {
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	var list api.WorkspaceListResult
	apiOK(t, dir, "workspace.list", api.EmptyParams{}, &list)
	source := list.Workspaces[0].WorkspaceID
	splitRight(t, dir)
	var result api.WorkspaceCreatedResult
	apiOK(t, dir, "workspace.set_section", map[string]any{"workspace_id": source, "section": "work"}, nil)
	apiOK(t, dir, "workspace.duplicate", map[string]any{"workspace_id": source, "focus": true}, &result)
	if result.Type != "workspace_created" || result.Tab.WorkspaceID != result.Workspace.WorkspaceID || result.RootPane.PaneID == "" || result.Workspace.WorkspaceID == source || !result.Workspace.Focused || result.Workspace.PaneCount != 2 {
		t.Fatal(result)
	}
	for _, method := range []string{"workspace.duplicate", "workspace.set_section"} {
		p := map[string]any{"workspace_id": "s9999"}
		if method == "workspace.set_section" {
			p["section"] = "work"
		}
		r := apiReq(t, dir, method, p)
		if r.Err == nil || r.Err.Code != api.ErrWorkspaceNotFound {
			t.Fatal(r)
		}
	}
	r := apiReq(t, dir, "workspace.set_section", map[string]any{"workspace_id": source, "section": "invalid"})
	if r.Err == nil {
		t.Fatal("invalid section accepted")
	}
}
