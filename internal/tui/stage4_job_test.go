package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func TestStage4JobClickNavigatesBeforeOpeningRealLog(t *testing.T) {
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir()})
	log := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(log, []byte("REAL_JOB_LOG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.jobRows = []render.JobRow{{CallerPane: "p1", LogPath: log, Cwd: a.cwd, Label: "test job"}}
	a.openJobLog(0)
	if a.st.Active != 0 || a.focusedPane() != 1 || a.st.Popup != nil {
		t.Fatal("first click did not navigate without a popup")
	}
	a.openJobLog(0)
	if a.st.Popup == nil {
		t.Fatal("second click did not open the log")
	}
	popup := a.st.Popup.PaneID
	t.Cleanup(func() { a.sup.Kill(popup) })
	specWaitText(t, a.sup.Get(popup), "REAL_JOB_LOG")
	a.exec(model.CmdClosePopup{})
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	a.openJobLog(0)
	if a.st.Popup != nil {
		t.Fatal("missing log opened a popup")
	}
	a.jobRows[0].CallerPane = "p999"
	a.openJobLog(0)
	if a.st.Popup != nil || a.st.Active != 0 {
		t.Fatal("missing caller acted")
	}
}
