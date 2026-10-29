package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestEndpointBothViewersRetainInputDuringSharedTabRedraw(t *testing.T) {
	for _, shell := range []bool{true, false} {
		name := "native-tee"
		if shell {
			name = "interactive-shell"
		}
		t.Run(name, func(t *testing.T) { endpointBothViewersRetainInput(t, shell) })
	}
}

func endpointBothViewersRetainInput(t *testing.T, shell bool) {
	teststate.Lock(t)
	useTestShellPrompt(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	waitRead(t, dir, pid, "ikesu-test-ready")
	ledger := filepath.Join(t.TempDir(), "input-ledger")
	t.Cleanup(func() {
		if t.Failed() {
			data, err := os.ReadFile(ledger)
			t.Logf("execution ledger: %q error=%v", data, err)
		}
	})
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, c := range []*testConn{first, second} {
		c.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
		c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			message, err := readClientMsg(c.rd)
			if err != nil {
				t.Fatalf("first snapshot: %v", err)
			}
			if message.Type == "snapshot" {
				break
			}
		}
		c.conn.SetReadDeadline(time.Time{})
		go func(client *testConn) {
			io.Copy(io.Discard, client.rd)
			drained <- struct{}{}
		}(c)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	if shell {
		// Preserve the original interactive shell and canonical input path. The
		// printf command still executes there, and its actual output is also saved.
		setup := fmt.Sprintf("L=%q; printf() { command printf \"$@\" | /usr/bin/tee -a \"$L\"; }; command printf 'INPUT_%%s\\n' READY\n", ledger)
		first.send(t, clientMsg{Type: "input", Data: encode64([]byte(setup))})
	} else {
		first.send(t, clientMsg{Type: "input", Data: encode64([]byte(fmt.Sprintf("/bin/stty -echo -icanon min 1 time 0; printf 'INPUT_%%s\\n' READY; exec /usr/bin/tee %q\n", ledger)))})
	}
	waitRead(t, dir, pid, "INPUT_READY")
	// Neither viewer acknowledges subsequent redraws. Resize and shell input
	// alternate between them after each has received its first real snapshot.
	for index := 0; index < 10; index++ {
		for _, row := range []struct {
			client *testConn
			name   string
			cols   int
		}{{first, "FIRST", 80}, {second, "SECOND", 160}} {
			row.client.send(t, clientMsg{Type: "resize", Cols: row.cols, Rows: 45})
			command := fmt.Sprintf("%s_%02d\n", row.name, index)
			if shell {
				command = fmt.Sprintf("printf '%s_%%s\\n' %02d\n", row.name, index)
			}
			row.client.send(t, clientMsg{Type: "input", Data: encode64([]byte(command))})
		}
	}
	// Keep all screen assertions, and independently verify exact receipt and
	// each viewer's ordering from the real PTY process's durable byte stream.
	waitPromptCondition(t, func() bool {
		data, _ := os.ReadFile(ledger)
		return strings.Count(string(data), "\n") == 20
	})
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	next := map[string]int{"FIRST": 0, "SECOND": 0}
	for _, marker := range strings.Fields(string(data)) {
		name, _, found := strings.Cut(marker, "_")
		index, known := next[name]
		if !found || !known || marker != fmt.Sprintf("%s_%02d", name, index) {
			t.Fatalf("lost, duplicated or reordered input: %q", data)
		}
		next[name]++
	}
	if next["FIRST"] != 10 || next["SECOND"] != 10 {
		t.Fatalf("incomplete input: %q", data)
	}
	if shell {
		first.send(t, clientMsg{Type: "input", Data: encode64([]byte("command cat \"$L\"\n"))})
	}
	for index := 0; index < 10; index++ {
		for _, name := range []string{"FIRST", "SECOND"} {
			waitRead(t, dir, pid, fmt.Sprintf("%s_%02d", name, index))
		}
	}
}

func TestEndpointOnlyFocusedVisibleTabsBecomeRead(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	firstPane := firstPaneID(t, dir)
	var secondTab, hiddenTab api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &secondTab)
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &hiddenTab)
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, c := range []*testConn{first, second} {
		c.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40})
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
		c.conn.SetReadDeadline(time.Time{})
		go func(client *testConn) { io.Copy(io.Discard, client.rd); drained <- struct{}{} }(c)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x02nprintf 'SECOND_%s\\n' VISIBLE\n"))})
	waitRead(t, dir, secondTab.RootPane.PaneID, "SECOND_VISIBLE")
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[O"))})
	// A following shell marker proves the blur was handled before completion.
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'FIRST_%s\\n' BLURRED\n"))})
	waitRead(t, dir, firstPane, "FIRST_BLURRED")
	panes := []string{firstPane, secondTab.RootPane.PaneID, hiddenTab.RootPane.PaneID}
	for _, pane := range panes {
		for _, state := range []string{"working", "idle"} {
			apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: state}, nil)
		}
	}
	assertStatus := func(pane, want string) {
		t.Helper()
		var result api.PaneInfoResult
		apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: pane}, &result)
		if result.Pane.AgentStatus != want {
			t.Fatalf("%s status=%s, want %s", pane, result.Pane.AgentStatus, want)
		}
	}
	assertStatus(firstPane, "done")
	assertStatus(secondTab.RootPane.PaneID, "idle")
	assertStatus(hiddenTab.RootPane.PaneID, "done")
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[Iprintf 'FIRST_%s\\n' REFOCUSED\n"))})
	waitRead(t, dir, firstPane, "FIRST_REFOCUSED")
	assertStatus(firstPane, "idle")
	assertStatus(secondTab.RootPane.PaneID, "idle")
	assertStatus(hiddenTab.RootPane.PaneID, "done")
	var tabs api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tabs)
	if !tabs.Tabs[0].Focused || tabs.Tabs[1].Focused || tabs.Tabs[2].Focused {
		t.Fatal("viewer navigation changed the session focus", tabs)
	}
}
