package input

import (
	"fmt"
	"testing"
)

func TestNegativeMouseReportsNeverBecomeKeys(t *testing.T) {
	for _, prefix := range []string{"\x1b[<", "\x9b<"} {
		for _, report := range []string{"0;-8;24M", "32;333;-8M", "0;333;-8m", "32;-8;-8M"} {
			raw := prefix + report
			for split := 0; split <= len(raw); split++ {
				t.Run(fmt.Sprintf("%x/%d", raw, split), func(t *testing.T) {
					f := &Framer{}
					events := f.Push([]byte(raw[:split]))
					events = append(events, f.Push([]byte(raw[split:]+"z"))...)
					keys := ""
					for _, event := range events {
						if event.Kind == KindKey {
							keys += string(event.Raw)
						} else if event.Kind != KindUnknown {
							t.Fatalf("negative report became an event: %+v", event)
						}
					}
					if keys != "z" || f.Pending() {
						t.Fatalf("leaked keys=%q pending=%v", keys, f.Pending())
					}
				})
			}
		}
	}
}

func TestNegativeMouseFragmentKeepsMouseTimeout(t *testing.T) {
	f := &Framer{}
	if events := f.Push([]byte("\x1b[<32;333;-8")); len(events) != 0 || f.TimeoutMs() != MouseFlushMs {
		t.Fatalf("fragment events=%+v timeout=%d", events, f.TimeoutMs())
	}
	if events := f.FlushTimeout(); len(events) != 0 || f.Pending() {
		t.Fatalf("expired fragment events=%+v pending=%v", events, f.Pending())
	}
	if events := f.Push([]byte("[<32;-8;24M")); len(events) != 0 || f.Pending() {
		t.Fatalf("orphan negative report leaked: %+v", events)
	}
}
