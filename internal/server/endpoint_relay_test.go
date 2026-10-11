package server

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/client"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointSSHRelayForwardsRealServerAndStopsOnStdinEOF(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	input, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer write.Close()
	read, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer output.Close()
	read.SetReadDeadline(time.Now().Add(5 * time.Second))
	finished := make(chan error, 1)
	go func() { finished <- client.Relay("test", input, output) }()
	encoder := json.NewEncoder(write)
	if err := encoder.Encode(clientMsg{Peer: endpoint.Current(), Type: "hello", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(read)
	welcome, err := readClientMsg(reader)
	if err != nil || welcome.Type != "welcome" {
		t.Fatal("relay changed the negotiated endpoint welcome", welcome, err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			if _, err := readClientMsg(reader); err != nil {
				return
			}
		}
	}()
	if err := encoder.Encode(clientMsg{Type: "input", Data: encode64([]byte("printf 'RELAY_%s\\n' REAL_PTY\n"))}); err != nil {
		t.Fatal(err)
	}
	waitRead(t, dir, pid, "RELAY_REAL_PTY")
	write.Close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay remained alive after SSH stdin EOF")
	}
	output.Close()
	<-drained
	if err := client.Relay("absent", input, output); err == nil || !strings.Contains(err.Error(), "server unavailable") {
		t.Fatal("missing remote session did not fail closed", err)
	}
}
