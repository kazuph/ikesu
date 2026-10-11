package runtime

import (
	"bytes"
	"github.com/charmbracelet/x/ansi"
)

// ReplayHistory affects the saved screen only: it never sends old commands
// to the new process or interprets saved OSC clipboard/title requests.
func (p *Pane) ReplayHistory(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeRecorded(historyDisplayBytes([]byte(text)))
	p.revision.Add(1)
}

// Saved output may carry styles and links, but never terminal requests
// affecting the attached host or the replacement process.
func historyDisplayBytes(data []byte) []byte {
	var out []byte
	parser := ansi.NewParser()
	state := byte(ansi.NormalState)
	for len(data) > 0 {
		seq, _, n, next := ansi.DecodeSequence(data, state, parser)
		if n <= 0 {
			break
		}
		state = next
		data = data[n:]
		if len(seq) == 0 {
			continue
		}
		if seq[0] >= 32 && seq[0] != 127 && seq[0] != ansi.CSI && seq[0] != ansi.OSC || bytes.Equal(seq, []byte("\r")) || bytes.Equal(seq, []byte("\n")) || bytes.HasPrefix(seq, []byte("\x1b[")) && seq[len(seq)-1] == 'm' || bytes.HasPrefix(seq, []byte("\x1b]8;")) {
			out = append(out, seq...)
		}
	}
	return out
}
