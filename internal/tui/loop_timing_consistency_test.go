package tui

import (
	"fmt"
	"testing"
	"time"
)

func TestLoopTimingSnapshotConsistentDuringRecovery(t *testing.T) {
	a := &App{loopTiming: &loopTiming{}}
	a.loopTiming.advance(time.Unix(0, int64(LoopStallThreshold)))
	finished := make(chan struct{})
	readerDone := make(chan struct{})
	errors := make(chan error, 1)
	go func() {
		defer close(finished)
		for i := int64(2); i <= 1000000; i++ {
			a.loopTiming.advance(time.Unix(0, i*int64(LoopStallThreshold)))
		}
	}()
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-finished:
				return
			default:
			}
			heartbeat, count, _ := a.LoopTiming()
			generation := uint64(heartbeat.UnixNano() / int64(LoopStallThreshold))
			if count >= generation {
				select {
				case errors <- fmt.Errorf("heartbeat generation %d but completed count %d", generation, count):
				default:
				}
				return
			}
		}
	}()
	<-finished
	<-readerDone
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}
