package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/worktree"
)

func TestWorktreeResultsPreserveNewDialog(t *testing.T) {
	teststate.Lock(t)
	for _, op := range []string{"source", "list", "create_failure"} {
		t.Run(op, func(t *testing.T) {
			a, repo := worktreeApp(t)
			switch op {
			case "source":
				a.newWorktreeDialog(0)
			case "list":
				a.openWorktreeMenu(0)
			case "create_failure":
				src, err := worktree.Source(repo)
				if err != nil {
					t.Fatal(err)
				}
				req, _ := a.workspaceWorktreeRequest(0, "create")
				a.showNewWorktreeDialog(req, src)
				*a.dialog.input = "bad..branch"
				old := a.dialog
				a.dialog = nil
				old.run(a)
			}
			run := func() { job := <-a.jobs; job.fn(); close(job.done) }
			run() // worktree request snapshot
			input := "keep this input"
			current := &uiDialog{input: &input, d: render.Dialog{Title: "Other dialog"}}
			a.dialog = current
			switch op {
			case "source", "list":
				run()
				run()
			case "create_failure":
				run()
				run()
				run()
			}
			if a.dialog != current || *current.input != "keep this input" || a.menu != nil {
				t.Fatalf("%s replaced the newer dialog or opened a menu", op)
			}
		})
	}
}
