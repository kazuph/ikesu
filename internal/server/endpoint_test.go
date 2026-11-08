package server

import (
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestEndpointFocusReturnRetainsImmediateKeys(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	pid := firstPaneID(t, dir)
	client.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	client.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[O\x1b[Iprintf 'FOCUS_%s\\n' KEPT\n"))})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool {
		data, _ := decode64(m.Data)
		return strings.Contains(string(data), "FOCUS_KEPT")
	})
	waitRead(t, dir, pid, "FOCUS_KEPT")
}

func TestEndpointRejectsGenerationBeforeAttach(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Peer: endpoint.Peer{Protocol: 1}, Cols: 80, Rows: 24})
	messages := client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "error" })
	if len(messages) != 1 || !strings.Contains(messages[0].Reason, "endpoint_generation_unsupported: upgrade") {
		t.Fatal(messages)
	}
}
