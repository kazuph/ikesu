package server

import (
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/latency"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptTraceEndsOnceWithoutDiscardingFunctionalWait(t *testing.T) {
	old := latency.Enabled
	latency.Enabled = true
	defer func() { latency.Enabled = old }()
	path := filepath.Join(t.TempDir(), "server.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	tr := latency.New(file)
	s := &Server{trace: tr}
	for i, reason := range []string{"ack", "timeout", "miss"} {
		revision := uint64(i + 1)
		c := &clientConn{id: 17, graphics: &graphicsFlow{awaiting: revision, until: time.Now(), traceAwaiting: revision, traceStarted: time.Now().Add(-graphicsReceiptWait)}}
		s.traceReceiptEnd(c, reason)
		s.traceReceiptEnd(c, reason)
		if c.graphics.awaiting != revision {
			t.Fatal("trace discarded functional receipt")
		}
	}
	tr.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"ack", "timeout", "miss"} {
		if strings.Count(string(data), "reason="+reason) != 1 {
			t.Fatal("receipt end count", reason, string(data))
		}
	}
	if strings.Count(string(data), "waited_ms=") != 3 {
		t.Fatal("missing receipt duration")
	}
}

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
