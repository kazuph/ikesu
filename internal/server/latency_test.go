package server

import (
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/latency"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiptTraceEarlyAckStartsAndEndsAfterPublication(t *testing.T) {
	old := latency.Enabled
	latency.Enabled = true
	defer func() { latency.Enabled = old }()
	path := filepath.Join(t.TempDir(), "early.log")
	tr := latency.Open(path)
	s := &Server{trace: tr}
	c := &clientConn{id: 12, graphics: &graphicsFlow{awaiting: 5, writing: true}}
	c.graphics.encoder.GraphicsPatches = true
	s.graphicsControl(c, clientMsg{Kind: endpoint.GraphicsPatchAck, Data: "5"})
	if c.graphics.awaiting != 0 || c.graphics.traceAwaiting != 0 {
		t.Fatal("early ack left functional wait or premature trace")
	}
	s.traceReceiptDrained(c, 5, true)
	s.traceReceiptDrained(c, 5, true)
	tr.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "event=receipt.started") != 1 || strings.Count(string(data), "event=receipt.ended") != 1 || !strings.Contains(string(data), "reason=ack revision=5") {
		t.Fatal("early ack trace", string(data))
	}
}
