package render

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestOutputFenceStatusReplyAndOneSecondLimit(t *testing.T) {
	now := time.Unix(100, 0)
	var fence OutputFence
	if fence.Written([]byte("text-only"), now) || fence.Waiting(now) {
		t.Fatal("text-only output waited on an image status")
	}
	if !fence.Written([]byte("\x1b_Ga=t,t=d;pixels\x1b\\"), now) || !fence.Deadline().Equal(now.Add(time.Second)) {
		t.Fatal("image output did not use the specified one-second limit")
	}
	if !fence.Waiting(now.Add(time.Second-time.Nanosecond)) || fence.Waiting(now.Add(time.Second)) {
		t.Fatal("terminal status wait expired at the wrong boundary")
	}
	fence.Written([]byte("\x1b_Ga=p,i=1;\x1b\\"), now)
	if fence.Accept([]byte("x")) || !fence.Waiting(now) {
		t.Fatal("a pane key released the terminal status wait")
	}
	if !fence.Accept([]byte("\x1b[0n")) || fence.Waiting(now) || !fence.Deadline().IsZero() {
		t.Fatal("terminal completion did not release the wait")
	}
	if !fence.Accept([]byte("\x1b[0n")) {
		t.Fatal("a late terminal reply could leak to a pane")
	}
}

func TestOutputFenceAcceptsPaneTerminalStatusReply(t *testing.T) {
	now := time.Unix(100, 0)
	var fence OutputFence
	if !fence.Written([]byte("\x1b_Ga=p,i=1;\x1b\\"), now) || !fence.Waiting(now) {
		t.Fatal("image placement did not start a terminal status wait")
	}
	if fence.Accept([]byte("\x1b[?0n")) || !fence.Waiting(now) {
		t.Fatal("DEC status incorrectly released the ANSI status wait")
	}
	outer := vt.NewEmulator(80, 24)
	defer outer.Close()
	reply := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := outer.Read(buf)
		if err != nil {
			reply <- nil
			return
		}
		reply <- buf[:n]
	}()
	// An ikesu running inside a pane writes this query to the outer emulator.
	if _, err := outer.WriteString(TerminalStatusQuery); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-reply:
		if !fence.Accept(raw) || fence.Waiting(now) || !fence.Deadline().IsZero() {
			t.Fatalf("pane terminal reply %q did not release the image wait", raw)
		}
	case <-time.After(TerminalStatusWait):
		t.Fatal("pane terminal did not answer the status query")
	}
}
