package client

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
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
	two := "\x1b]0;gtx\x07\x1b]0;gtx\x07"
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
