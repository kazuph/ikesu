package model

import (
	"encoding/json"
	"testing"
)

func workspaceIdentity(t *testing.T, ws *Workspace) string {
	t.Helper()
	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	id, _ := fields["Generation"].(string)
	return id
}

func TestWorkspaceIdentitySurvivesRestoreAndDiffersForDuplicate(t *testing.T) {
	s := New(t.TempDir(), 80, 24)
	original := workspaceIdentity(t, s.Workspaces[0])
	if original == "" {
		t.Fatal("workspace has no identity")
	}
	restored := Restore(s.Snapshot())
	if got := workspaceIdentity(t, restored.Workspaces[0]); got != original {
		t.Fatalf("identity after restore = %q, want %q", got, original)
	}
	restored.Apply(CmdDuplicateWorkspace{Index: 0})
	if got := workspaceIdentity(t, restored.Workspaces[1]); got == "" || got == original {
		t.Fatalf("duplicate identity = %q, original %q", got, original)
	}
}
