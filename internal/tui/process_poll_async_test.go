package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRunningAppObservesShellCwdAndStops(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(a.focusedPane())
	done := make(chan struct{})
	go func() { a.Run(); close(done) }()
	defer func() {
		a.Stop()
		<-done
	}()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.PushInput([]byte("cd '" + dir + "'; printf 'ASYNC_CWD_READY\\n'\n"))
	specWaitText(t, p, "ASYNC_CWD_READY")
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for p.Snapshot().Cwd != dir {
		select {
		case <-deadline.C:
			t.Fatalf("running app did not observe cwd: got %q, want %q", p.Snapshot().Cwd, dir)
		case <-tick.C:
		}
	}
}

func TestHandoffWaitsForProcessPollWithoutHoldingAppLoop(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { a.Run(); close(done) }()
	defer func() { a.Stop(); <-done }()
	<-a.processPollPermit
	held := true
	defer func() {
		if held {
			a.processPollPermit <- struct{}{}
		}
	}()
	handoff := make(chan struct{})
	go a.DoWithProcessPollingPaused(func() { close(handoff) })
	loop := make(chan struct{})
	go a.Do(func() { close(loop) })
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case <-loop:
	case <-deadline.C:
		t.Fatal("waiting for process observation held the app loop")
	}
	select {
	case <-handoff:
		t.Fatal("handoff proceeded while process observation was active")
	default:
	}
	a.processPollPermit <- struct{}{}
	held = false
	select {
	case <-handoff:
	case <-deadline.C:
		t.Fatal("handoff did not resume after process observation")
	}
}
