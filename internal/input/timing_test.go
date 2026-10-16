package input

import "testing"

func TestEscapeTimeoutDependsOnInputRoute(t *testing.T) {
	for _, wait := range []int{0, EndpointEscapeFlushMs} {
		f := Framer{EscapeWaitMs: wait}
		f.Push([]byte{0x1b})
		want := IdleFlushMs
		if wait != 0 {
			want = wait
		}
		if f.TimeoutMs() != want {
			t.Fatalf("timeout=%d want=%d", f.TimeoutMs(), want)
		}
	}
}

func TestMouseUsesLongerInputTimeout(t *testing.T) {
	var f Framer
	f.Push([]byte("\x1b[<0;70;"))
	if f.TimeoutMs() != 150 {
		t.Fatalf("mouse timeout=%d want=150", f.TimeoutMs())
	}
	events := f.Push([]byte("20M"))
	if len(events) != 1 || events[0].Kind != KindMouseClick {
		t.Fatalf("completed mouse=%+v", events)
	}
}

func TestExpiredMouseLeavesLaterInputUntouched(t *testing.T) {
	var f Framer
	f.Push([]byte("\x1b[<0;70;"))
	if events := f.FlushTimeout(); len(events) != 0 {
		t.Fatalf("expired fragment=%+v", events)
	}
	events := f.Push([]byte("20M"))
	if len(events) != 3 || string(events[0].Raw)+string(events[1].Raw)+string(events[2].Raw) != "20M" {
		t.Fatalf("later input=%+v", events)
	}
}
