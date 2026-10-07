package runtime

import "testing"

func TestNotifyChangedCoalesces(t *testing.T) {
	ch := make(chan Event, 8)
	p := &Pane{ID: 1, ev: ch}

	p.notifyChanged()
	p.notifyChanged()
	p.notifyChanged()
	if n := len(ch); n != 1 {
		t.Fatalf("pending events=%d, want 1", n)
	}
	ev := <-ch
	if ev.Kind != EventChanged || ev.Pane != 1 {
		t.Fatalf("got %+v, want EventChanged for pane 1", ev)
	}

	// Consumed like the TUI: further changes must notify again.
	p.MarkChangedRead()
	p.notifyChanged()
	if n := len(ch); n != 1 {
		t.Fatalf("after ack pending events=%d, want 1", n)
	}
}

func TestNotifyChangedFullChannelRetries(t *testing.T) {
	ch := make(chan Event, 1)
	ch <- Event{Pane: 9, Kind: EventStatus}
	p := &Pane{ID: 1, ev: ch}

	// Send fails while the channel is full; the flag must reset so the
	// next change tries again instead of being dropped forever.
	p.notifyChanged()
	<-ch
	p.notifyChanged()
	select {
	case ev := <-ch:
		if ev.Kind != EventChanged || ev.Pane != 1 {
			t.Fatalf("got %+v, want EventChanged for pane 1", ev)
		}
	default:
		t.Fatal("no event after channel drained")
	}
}

// TestNotifyChangedNoLostUpdate models pump (notifyChanged) against the
// TUI consumer (read event, ack, draw latest). The last screen change must
// always end up drawn — no matter how the two interleave.
func TestNotifyChangedNoLostUpdate(t *testing.T) {
	for _, drainEvery := range []int{1, 3, 7, 1000} {
		ch := make(chan Event, 4)
		p := &Pane{ID: 1, ev: ch}
		screen, drawn := 0, 0
		drain := func() {
			for len(ch) > 0 {
				<-ch
				p.MarkChangedRead()
				drawn = screen
			}
		}
		for i := 0; i < 500; i++ {
			screen++
			p.notifyChanged()
			if i%drainEvery == 0 {
				drain()
			}
		}
		drain()
		if drawn != screen {
			t.Fatalf("drainEvery=%d: final draw saw %d, screen is %d — last output would never be drawn", drainEvery, drawn, screen)
		}
	}
}
