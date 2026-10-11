package server

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointProgramClipboardUsesLastVisibleViewerWithoutTruncation(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	var other api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &other)
	first, second := dialClient(t, dir), dialClient(t, dir)
	clients := []*testConn{first, second}
	copies := make([][]string, 2)
	received := []chan string{make(chan string, 8), make(chan string, 8)}
	var drained sync.WaitGroup
	drained.Add(2)
	for index, client := range clients {
		client.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40})
		client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
		client.conn.SetReadDeadline(time.Time{})
		go func(index int, c *testConn) {
			defer drained.Done()
			for {
				message, err := readClientMsg(c.rd)
				if err != nil {
					return
				}
				if message.Type == "output" {
					data, err := decode64(message.Data)
					if err == nil && strings.HasPrefix(string(data), "\x1b]52;") {
						copies[index] = append(copies[index], string(data))
						received[index] <- string(data)
					}
				}
			}
		}(index, client)
	}
	defer func() { first.conn.Close(); second.conn.Close(); drained.Wait() }()
	input := func(client *testConn, bytes string) {
		client.send(t, clientMsg{Type: "input", Data: encode64([]byte(bytes))})
	}
	marker := func(client *testConn, pane, text string) {
		input(client, "printf '"+text+"\\n'\n")
		waitRead(t, dir, pane, text)
	}
	emitted := 0
	emit := func(text string, long bool) string {
		encoded := encode64([]byte(text))
		command := "printf '\\033]52;c;" + encoded + "\\007'"
		if long {
			// Generate a real PTY payload beyond the title observer's cap,
			// without exceeding the shell's canonical input line limit.
			command = "python3 -c 'import base64,sys; sys.stdout.write(\"\\033]52;c;\"+base64.b64encode(b\"x\"*8192).decode()+\"\\007\")'"
		}
		emitted++
		command += fmt.Sprintf("; printf 'CLIP_SENT_%%s\\n' %d", emitted)
		apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pid, Text: command, Keys: []string{"enter"}}, nil)
		waitRead(t, dir, pid, fmt.Sprintf("CLIP_SENT_%d", emitted))
		return "\x1b]52;c;" + encoded + "\a"
	}
	expect := func(index int, text string) {
		t.Helper()
		select {
		case got := <-received[index]:
			if got != text {
				t.Fatalf("viewer %d clipboard bytes differ: got %d, want %d", index, len(got), len(text))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("viewer %d did not receive clipboard", index)
		}
	}
	secondCopy := emit("LAST_ATTACHED", false)
	expect(1, secondCopy)
	marker(first, pid, "FIRST_USED")
	firstCopy := emit(strings.Repeat("x", 8192), true)
	expect(0, firstCopy)
	input(second, "\x02n")
	marker(second, other.RootPane.PaneID, "SECOND_OTHER_TAB")
	visibleCopy := emit("OLDER_BUT_VISIBLE", false)
	expect(0, visibleCopy)
	input(first, "\x02n")
	marker(first, other.RootPane.PaneID, "BOTH_OTHER_TAB")
	emit("HIDDEN_MUST_NOT_COPY", false)
	input(second, "\x02p")
	marker(second, pid, "SECOND_RETURNED")
	returnedCopy := emit("VISIBLE_AGAIN", false)
	expect(1, returnedCopy)
	first.conn.Close()
	second.conn.Close()
	drained.Wait()
	if !reflect.DeepEqual(copies, [][]string{{firstCopy, visibleCopy}, {secondCopy, returnedCopy}}) {
		t.Fatal("clipboard was duplicated or delivered to a hidden viewer", copies)
	}
}
