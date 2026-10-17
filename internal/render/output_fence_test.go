package render

import (
	"testing"
	"time"
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
