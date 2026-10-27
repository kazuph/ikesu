package tui

import (
	"bytes"
	"encoding/gob"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLiveHandoffKeepsLoadedWorkspaceLabel(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	repo := t.TempDir()
	sidebarGit(t, repo, "init", "-qb", "main")
	child := filepath.Join(repo, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	a.st.Workspaces[0].Cwd = child
	a.cwd = child
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	waitWorkspaceGitActivity(t, a, func() bool { return a.WorkspaceLabel(0) == filepath.Base(repo) })
	before := a.WorkspaceLabel(0)
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(a.HandoffState()); err != nil {
		t.Fatal(err)
	}
	var transfer HandoffState
	if err := gob.NewDecoder(&encoded).Decode(&transfer); err != nil {
		t.Fatal(err)
	}
	imported := NewApp(a.st, a.sup, io.Discard, child)
	imported.RestoreHandoff(transfer)
	if got := imported.WorkspaceLabel(0); got != before {
		t.Fatalf("loaded label changed during handoff: before=%q after=%q", before, got)
	}
	// A moved shell must still invalidate the transferred directory label.
	other := t.TempDir()
	p := a.sup.Get(a.focusedPane())
	if _, err := p.WriteToPTY([]byte("cd '" + other + "'; printf 'LABEL_%s\\n' MOVED\r")); err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "LABEL_MOVED")
	if got := imported.WorkspaceLabel(0); got != filepath.Base(other) {
		t.Fatalf("transferred cache hid shell cwd change: %q", got)
	}
}
