package server

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
)

func TestBufferedMotionsPreserveEveryOrderingBoundary(t *testing.T) {
	motion := clientMsg{Type: "input", Data: encode64([]byte("\x1b[<32;40;10M"))}
	for _, boundary := range []clientMsg{
		{Type: "input", Data: encode64([]byte("\x1b[<0;43;10m"))},
		{Type: "input", Data: encode64([]byte("\x1b[<0;43;10M"))},
		{Type: "input", Data: encode64([]byte("\x1b[<64;43;10M"))},
		{Type: "input", Data: encode64([]byte("x"))},
		{Type: "input", Data: motion.Data, EndPrefix: true},
		{Type: "input", Data: motion.Data, SidebarJobs: new(bool)},
		{Type: "resize", Cols: 160, Rows: 45},
		{Type: "workspace_select", Workspace: 2},
		{Type: endpoint.GraphicsResultMessage},
		{Type: "paste", Data: encode64([]byte("text"))},
	} {
		var wire strings.Builder
		for _, m := range []clientMsg{motion, boundary, motion} {
			b, _ := json.Marshal(m)
			wire.Write(b)
			wire.WriteByte('\n')
		}
		rd := bufio.NewReader(strings.NewReader(wire.String()))
		// Fill the reader exactly as the preceding readClientMsg does.
		if _, err := rd.Peek(wire.Len()); err != nil {
			t.Fatal(err)
		}
		joined, pending, err := bufferedMotions(rd, motion)
		data, _ := decode64(joined.Data)
		want, _ := decode64(motion.Data)
		if err != nil || string(data) != string(want)+string(want) || pending == nil {
			t.Fatal("motion batch or boundary lost", joined, pending, err)
		}
		got, _ := json.Marshal(pending)
		expected, _ := json.Marshal(boundary)
		if string(got) != string(expected) {
			t.Fatal("ordering boundary changed", string(got), string(expected))
		}
		next, err := readClientMsg(rd)
		if err != nil || next.Data != motion.Data {
			t.Fatal("motion after boundary was consumed", next, err)
		}
	}
}

func TestBufferedMotionsNeverWaitForIncompleteMessage(t *testing.T) {
	motion := clientMsg{Type: "input", Data: encode64([]byte("\x1b[<32;40;10M"))}
	rd := bufio.NewReader(strings.NewReader("{\"type\":\"input\""))
	_, _ = rd.Peek(len("{\"type\":\"input\""))
	before := rd.Buffered()
	got, pending, err := bufferedMotions(rd, motion)
	if err != nil || pending != nil || got.Data != motion.Data || before != rd.Buffered() {
		t.Fatal("incomplete input was consumed", got, pending, err)
	}
}
