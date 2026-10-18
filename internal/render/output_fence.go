package render

import (
	"bytes"
	"time"
)

// P4b bounds the wait for a terminal's image-processing status at one second.
const TerminalStatusWait = time.Second
const TerminalStatusQuery = "\x1b[5n"

type OutputFence struct{ until time.Time }

func (f *OutputFence) Waiting(now time.Time) bool {
	return now.Before(f.until)
}

func (f *OutputFence) Deadline() time.Time { return f.until }

// Written reports whether this frame needs a terminal completion query.
// Unchanged-image text updates do not start another image wait.
func (f *OutputFence) Written(data []byte, now time.Time) bool {
	if !bytes.Contains(data, []byte("\x1b_Ga=t,")) && !bytes.Contains(data, []byte("\x1b_Ga=p,")) {
		return false
	}
	f.until = now.Add(TerminalStatusWait)
	return true
}

func (f *OutputFence) Accept(raw []byte) bool {
	if !bytes.Equal(raw, []byte("\x1b[0n")) {
		return false
	}
	f.until = time.Time{}
	return true
}
