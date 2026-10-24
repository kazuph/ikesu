package server

import (
	"bufio"
	"bytes"

	"github.com/kazuph/ikesu/internal/input"
)

func motionBytes(m clientMsg) ([]byte, bool) {
	if m.Type != "input" || m.EndPrefix {
		return nil, false
	}
	data, err := decode64(m.Data)
	if err != nil {
		return nil, false
	}
	var fr input.Framer
	events := fr.Push(data)
	if len(events) == 0 || fr.Pending() {
		return nil, false
	}
	for _, ev := range events {
		if ev.Kind != input.KindMouseMotion {
			return nil, false
		}
	}
	return data, true
}

// Consume only complete messages already in the reader. The first non-motion
// is retained for the ordinary dispatcher, including graphics and disconnect
// boundaries. No socket read or new wait is introduced to collect input.
func bufferedMotions(rd *bufio.Reader, first clientMsg) (clientMsg, *clientMsg, error) {
	data, ok := motionBytes(first)
	if !ok {
		return first, nil, nil
	}
	merged := false
	for available := rd.Buffered(); available > 0; available = rd.Buffered() {
		peek, _ := rd.Peek(available)
		if !bytes.ContainsRune(peek, '\n') {
			break
		}
		next, err := readClientMsg(rd)
		if err != nil {
			if merged {
				first.Data = encode64(data)
			}
			return first, nil, err
		}
		more, motion := motionBytes(next)
		samePanel := first.SidebarJobs == nil && next.SidebarJobs == nil || first.SidebarJobs != nil && next.SidebarJobs != nil && *first.SidebarJobs == *next.SidebarJobs
		if !motion || !samePanel {
			if merged {
				first.Data = encode64(data)
			}
			return first, &next, nil
		}
		data = append(data, more...)
		merged = true
	}
	if merged {
		first.Data = encode64(data)
	}
	return first, nil, nil
}
