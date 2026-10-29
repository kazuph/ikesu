package server

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientReadUntilPreservesSplitMessageAcrossDeadlines(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "ikesu-read-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	listener, err := net.Listen("unix", filepath.Join(root, "client.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	expected := clientMsg{Type: "output", Data: encode64([]byte("分割された表示\n"))}
	wire, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, '\n')
	split := len(wire) / 2
	if _, err := peer.Write(wire[:split]); err != nil {
		t.Fatal(err)
	}
	client := &testConn{conn: conn, rd: bufio.NewReader(conn)}
	predicate := func(message clientMsg) bool { return message.Type == "output" }
	messages, err := client.readUntilResult(time.Millisecond, predicate)
	if err != nil || len(messages) != 0 {
		t.Fatalf("partial message completed: %+v %v", messages, err)
	}
	if _, err := peer.Write(wire[split:]); err != nil {
		t.Fatal(err)
	}
	messages, err = client.readUntilResult(time.Second, predicate)
	if err != nil || len(messages) != 1 || messages[0].Type != expected.Type || messages[0].Data != expected.Data {
		t.Fatalf("split message lost across read calls: %+v %v", messages, err)
	}
}
