package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
)

// Coverage counts show how many times drawing enters Git metadata discovery.
func TestBranchRepeatedPaneSnapshots(t *testing.T) {
	teststate.Lock(t)
	a, repo := worktreeApp(t)
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, repo, 80, 24, nil, []string{"/bin/sh", "-c", "printf READY; exec cat"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	for i := 0; i < 100; i++ {
		if got := a.paneSnaps()[pid].Branch; got == "" {
			t.Fatal("missing branch")
		}
	}
}
