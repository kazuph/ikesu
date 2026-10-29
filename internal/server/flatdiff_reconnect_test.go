package server

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestFlatDiffRealSocketReconnectReseedsAndNegotiatesLegacy(t *testing.T) {
	teststate.Lock(t)
	useTestShellPrompt(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	content := func(in render.Input) string {
		var b strings.Builder
		for _, p := range in.Panes {
			for _, row := range p.Grid {
				for _, cell := range row {
					b.WriteString(cell.Content)
				}
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
	connect := func(legacy bool, marker string) (*testConn, render.Input) {
		t.Helper()
		c := dialClient(t, dir)
		t.Cleanup(func() { c.conn.Close() })
		peer := endpoint.Current()
		if legacy {
			peer.SurfaceCodecs = nil
		}
		c.send(t, clientMsg{Peer: peer, Type: "hello", Cols: 80, Rows: 24})
		if err := c.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var decoder endpoint.SnapshotDecoder
		var auditBuffer bytes.Buffer
		audit := gob.NewDecoder(&auditBuffer)
		for {
			m, err := readClientMsg(c.rd)
			if err != nil {
				t.Fatal(err)
			}
			if m.Type != "snapshot" {
				continue
			}
			data, err := decode64(m.Data)
			if err != nil {
				t.Fatal(err)
			}
			auditBuffer.Write(data)
			var shape struct {
				Input     render.Input
				FlatGrids map[int]struct{ Records []byte }
			}
			if err := audit.Decode(&shape); err != nil {
				t.Fatal(err)
			}
			if (len(shape.FlatGrids) != 0) != (endpoint.ExperimentalFlatDiff && !legacy) {
				t.Fatal("peer negotiation selected the wrong wire format", legacy, len(shape.FlatGrids))
			}
			in, err := decoder.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if marker == "" || strings.Contains(content(in), marker) {
				return c, in
			}
		}
	}
	first, _ := connect(false, "ikesu-test-ready")
	pid := firstPaneID(t, dir)
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'RESEED_%s\\n' FIRST\n"))})
	// Reconnect after the real shell has produced the content to reseed.
	// Closing immediately also races delivery of the preceding input message.
	waitRead(t, dir, pid, "RESEED_FIRST")
	first.conn.Close()
	second, saved := connect(false, "RESEED_FIRST")
	before := content(saved)
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'RESEED_%s\\n' SECOND\n"))})
	waitRead(t, dir, pid, "RESEED_SECOND")
	second.conn.Close()
	third, _ := connect(false, "RESEED_SECOND")
	third.conn.Close()
	legacy, _ := connect(true, "RESEED_SECOND")
	legacy.conn.Close()
	if content(saved) != before {
		t.Fatal("reconnecting changed an earlier snapshot")
	}
}
