package input

import "testing"

func TestColorReplyGetsOneAdditionalEscapeWindow(t *testing.T) {
	f := Framer{ColorReportPending: true}
	f.Push([]byte{0x1b})
	if events := f.FlushTimeout(); len(events) != 0 {
		t.Fatalf("pending host reply emitted Escape: %+v", events)
	}
	events := f.Push([]byte("]11;rgb:1111/2222/3333\x1b\\"))
	if len(events) != 1 || events[0].Kind != KindTerminalReport || f.ColorReportPending {
		t.Fatalf("host reply=%+v pending=%v", events, f.ColorReportPending)
	}
	f.ColorReportPending = true
	f.Push([]byte{0x1b})
	f.FlushTimeout()
	events = f.FlushTimeout()
	if len(events) != 1 || events[0].Kind != KindKey || string(events[0].Raw) != "\x1b" {
		t.Fatalf("Escape must not wait indefinitely: %+v", events)
	}
}

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
	f := Framer{EscapeWaitMs: EndpointEscapeFlushMs, ColorReportPending: true}
	f.Push([]byte{0x1b})
	f.FlushTimeout()
	if f.TimeoutMs() != IdleFlushMs {
		t.Fatalf("additional color wait=%d want=%d", f.TimeoutMs(), IdleFlushMs)
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
