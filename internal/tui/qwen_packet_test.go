package tui

import (
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/status"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestQwenApprovalPublishesBlockedAvailabilityFromRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.st.FocusedPane()
	r := a.rectForPane(pid)
	p, err := a.sup.SpawnWith(pid, a.st.ActiveWorkspace().Cwd, r.W, r.H, nil, []string{"/bin/sh", "-c", "printf 'apply this change?\\nyes, allow once\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.sup.Kill(pid)
	specWaitText(t, p, "yes, allow once")
	p.SetAgent("qwen")
	blocked := false
	a.SetAvailHook(func(changed int) {
		if changed == pid && a.PaneStatusKey(pid) == "blocked" {
			blocked = true
		}
	})
	a.detectAll()
	<-time.NewTimer(status.StartupGrace).C
	a.detectAll()
	if !blocked || a.PaneStatusKey(pid) != "blocked" {
		t.Fatal("approval was not published to the notification availability hook")
	}
}
