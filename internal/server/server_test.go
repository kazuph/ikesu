package server

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

// testConn is a raw client-socket connection for tests.
type testConn struct {
	conn        net.Conn
	rd          *bufio.Reader
	snapshots   endpoint.SnapshotDecoder
	frames      render.Encoder
	lastRegions *render.Output
	lastFrame   *render.Frame
	lastInput   *render.Input
}

func dialClient(t *testing.T, dir string) *testConn {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", api.ClientSocketPath(dir))
		if err == nil {
			return &testConn{conn: conn, rd: bufio.NewReader(conn)}
		}
		if time.Now().After(deadline) {
			t.Fatalf("client socket never came up at %s: %v", dir, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *testConn) send(t *testing.T, m clientMsg) {
	t.Helper()
	if m.Type == "hello" && m.Protocol == 0 {
		m.Peer = endpoint.Current()
	}
	if err := json.NewEncoder(c.conn).Encode(m); err != nil {
		t.Fatalf("send %q: %v", m.Type, err)
	}
}

// readUntil returns all messages until pred matches or the deadline hits.
func (c *testConn) readUntil(t *testing.T, timeout time.Duration, pred func(clientMsg) bool) []clientMsg {
	t.Helper()
	var got []clientMsg
	var ackErr error
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		line, err := c.rd.ReadBytes('\n')
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ackErr != nil {
				t.Fatalf("render acknowledgement failed before terminal message: %v", ackErr)
			}
			return got // EOF
		}
		var m clientMsg
		if json.Unmarshal(line, &m) == nil {
			if m.Type == "snapshot" {
				data, err := decode64(m.Data)
				if err != nil {
					t.Fatal(err)
				}
				in, err := c.snapshots.Decode(data)
				if err != nil {
					t.Fatal(err)
				}
				c.lastInput = &in
				out := render.Compose(in)
				c.lastFrame = out.Frame
				m.Type, m.Data = "output", encode64(c.frames.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible))
				out.Frame = nil
				// Match the real client: acknowledge the first frame, an explicit
				// request, or changed regions rather than every snapshot.
				if ackErr == nil && (m.RequestAck || c.lastRegions == nil || !reflect.DeepEqual(c.lastRegions, out)) {
					ackErr = json.NewEncoder(c.conn).Encode(clientMsg{Type: "rendered", Rendered: out, FocusEpoch: m.FocusEpoch})
					if ackErr != nil && !errors.Is(ackErr, syscall.EPIPE) {
						t.Fatalf("render acknowledgement: %v", ackErr)
					}
					c.lastRegions = out
				}
			}
			got = append(got, m)
			if pred(m) {
				// A closed write half can coexist with a queued restart/exit message.
				// Require that real terminal message; never hide ACK errors on live reads.
				if ackErr != nil && m.Type != "restart" && m.Type != "exit" && m.Type != "detached" {
					t.Fatalf("render acknowledgement failed on live read: %v", ackErr)
				}
				return got
			}
		}
	}
	if ackErr != nil {
		t.Fatalf("render acknowledgement failed without terminal message: %v", ackErr)
	}
	return got
}

func outputs(msgs []clientMsg) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Type == "output" {
			if b, err := base64.StdEncoding.DecodeString(m.Data); err == nil {
				sb.Write(b)
			}
		}
	}
	return sb.String()
}

func startTestServer(t *testing.T) (dir string, done chan int) {
	return startTestEndpoint(t, endpoint.Current())
}

func startTestEndpoint(t *testing.T, peer endpoint.Peer) (dir string, done chan int) {
	t.Helper()
	pinTestConfig(t)
	// unix sockets cap sun_path around 104 bytes; t.TempDir() paths on
	// macOS exceed that, so use a short base under /tmp.
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir = api.SessionDir("test")
	done = make(chan int, 1)
	go func() {
		code, err := runEndpoint("test", "test-version", peer)
		if err != nil {
			t.Errorf("server.Run: %v", err)
		}
		done <- code
	}()
	return dir, done
}

func TestAttachDetachReattach(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)

	// Attach 1: hello, then drive the pane.
	c1 := dialClient(t, dir)
	c1.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	msgs := c1.readUntil(t, 10*time.Second, func(m clientMsg) bool {
		return m.Type == "output"
	})
	if outputs(msgs) == "" {
		t.Fatal("no output frames received")
	}

	// Type a command; the echo output should arrive over the socket.
	c1.send(t, clientMsg{Type: "input", Data: encode64([]byte("echo gtxmarker42\n"))})
	var sawMarker bool
	for i := 0; i < 20 && !sawMarker; i++ {
		msgs = c1.readUntil(t, time.Second, func(m clientMsg) bool {
			b, _ := base64.StdEncoding.DecodeString(m.Data)
			return m.Type == "output" && strings.Contains(string(b), "gtxmarker42")
		})
		sawMarker = strings.Contains(outputs(msgs), "gtxmarker42")
	}
	if !sawMarker {
		t.Fatal("pane output never echoed gtxmarker42")
	}

	// Detach via the observed Ctrl-B q binding.
	c1.send(t, clientMsg{Type: "input", Data: encode64([]byte{0x02, 'q'})})
	msgs = c1.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "detached" })
	var detached bool
	for _, m := range msgs {
		if m.Type == "detached" {
			detached = true
		}
	}
	if !detached {
		t.Fatal("expected a detached message")
	}
	c1.conn.Close()

	// Reattach: the server must still be running and repaint on demand.
	c2 := dialClient(t, dir)
	c2.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	msgs = c2.readUntil(t, 10*time.Second, func(m clientMsg) bool {
		b, _ := base64.StdEncoding.DecodeString(m.Data)
		return m.Type == "output" && strings.Contains(string(b), "gtxmarker42")
	})
	if !strings.Contains(outputs(msgs), "gtxmarker42") {
		t.Fatal("reattach did not repaint the pane contents")
	}
	c2.conn.Close()

	// Stop the server; it should exit and clean up.
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatalf("dial api: %v", err)
	}
	if err := json.NewEncoder(conn).Encode(map[string]any{
		"id": "t", "method": "server.stop", "params": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(conn)
	var resp api.Response
	line, err := rd.ReadBytes('\n')
	if err != nil {
		t.Fatalf("stop response: %v", err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Err != nil {
		t.Fatalf("server.stop error: %+v", resp.Err)
	}
	conn.Close()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("server exit code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit after server.stop")
	}
	if _, err := os.Stat(filepath.Join(dir, "gtx.sock")); !os.IsNotExist(err) {
		t.Error("gtx.sock not removed on shutdown")
	}
}

func TestSecondClientPreservesFirst(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	waitAPISocket(t, dir)
	pid := firstPaneID(t, dir)
	consume := func(c *testConn) <-chan string {
		ended := make(chan string, 1)
		c.conn.SetReadDeadline(time.Time{})
		go func() {
			for {
				m, err := readClientMsg(c.rd)
				if err != nil {
					ended <- "closed"
					return
				}
				if m.Type == "detached" {
					ended <- "detached"
					return
				}
			}
		}()
		return ended
	}
	c1 := dialClient(t, dir)
	defer c1.conn.Close()
	c1.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	c1.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	firstEnded := consume(c1)
	c2 := dialClient(t, dir)
	defer c2.conn.Close()
	c2.send(t, clientMsg{Type: "hello", Cols: 100, Rows: 30})
	c2.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	secondEnded := consume(c2)
	c1.send(t, clientMsg{Type: "input", Data: encode64([]byte("echo firststillattached\n"))})
	waitRead(t, dir, pid, "firststillattached")
	select {
	case reason := <-firstEnded:
		t.Fatal("first client disconnected", reason)
	default:
	}
	c2.send(t, clientMsg{Type: "input", Data: encode64([]byte{2, 'q'})})
	select {
	case reason := <-secondEnded:
		if reason != "detached" {
			t.Fatal("second client did not detach", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second client did not detach")
	}
	c1.send(t, clientMsg{Type: "input", Data: encode64([]byte("echo firstsurvivesdetach\n"))})
	waitRead(t, dir, pid, "firstsurvivesdetach")
	select {
	case reason := <-firstEnded:
		t.Fatal("peer detach affected first", reason)
	default:
	}
}

func TestServerExitsWhenLastPaneExits(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)

	c := dialClient(t, dir)
	c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })

	// Exit the only pane's shell: the app quits, the client sees "exit".
	c.send(t, clientMsg{Type: "input", Data: encode64([]byte("exit\n"))})
	msgs := c.readUntil(t, 10*time.Second, func(m clientMsg) bool { return m.Type == "exit" })
	var sawExit bool
	for _, m := range msgs {
		if m.Type == "exit" {
			sawExit = true
		}
	}
	c.conn.Close()
	if !sawExit {
		t.Fatal("client did not receive exit message")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit after last pane exited")
	}
}

func stopServer(t *testing.T, dir string, done chan int) {
	t.Helper()
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		return
	}
	json.NewEncoder(conn).Encode(map[string]any{"id": "t", "method": "server.stop", "params": map[string]any{}})
	bufio.NewReader(conn).ReadBytes('\n')
	conn.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestSecondServerRefused(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir) // wait until sockets are live

	_, err := Run("test", "test-version")
	if err == nil {
		t.Fatal("second server.Run succeeded; want lock refusal")
	}
	done2 := done

	// Clean up via API stop.
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatalf("dial api: %v", err)
	}
	json.NewEncoder(conn).Encode(map[string]any{"id": "t", "method": "server.stop", "params": map[string]any{}})
	bufio.NewReader(conn).ReadBytes('\n')
	conn.Close()
	select {
	case <-done2:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

func TestPingAndUnknownMethod(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)

	req := func(method string) api.Response {
		conn, err := net.Dial("unix", api.APISocketPath(dir))
		if err != nil {
			t.Fatalf("dial api: %v", err)
		}
		defer conn.Close()
		json.NewEncoder(conn).Encode(map[string]any{"id": "x", "method": method, "params": map[string]any{}})
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var resp api.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	r := req("ping")
	if r.Err != nil {
		t.Fatalf("ping error: %+v", r.Err)
	}
	r = req("nope.method")
	if r.Err == nil || r.Err.Code != api.ErrUnknownMethod {
		t.Fatalf("nope.method should be unknown_method, got %+v", r.Err)
	}
	r = req("server.stop")
	if r.Err != nil {
		t.Fatalf("stop error: %+v", r.Err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

// A client holding the spawn lock while it forks must not block the new
// server from taking its own liveness lock (the two used to share one
// file, which made the child exit "already running" under load).
func TestServerStartsWhileSpawnLockHeld(t *testing.T) {
	teststate.Lock(t)
	base, err := os.MkdirTemp("/tmp", "gtxtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "GTX_STATE_DIR", base)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir := api.SessionDir("test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lf, err := os.OpenFile(api.SpawnLockPath(dir), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if err := unix.Flock(int(lf.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lf.Fd()), unix.LOCK_UN)

	done := make(chan int, 1)
	go func() {
		code, err := Run("test", "test-version")
		if err != nil {
			t.Errorf("server.Run under held spawn lock: %v", err)
		}
		done <- code
	}()
	waitAPISocket(t, dir)
	stopServer(t, dir, done)
}

// A client that stops reading must not stall the server: net.Pipe blocks
// every Write until the peer reads, so the wedged client hits the write
// deadline deterministically and is dropped.
func TestWedgedClientIsDropped(t *testing.T) {
	teststate.Lock(t)
	old := clientWriteTimeout
	clientWriteTimeout = 100 * time.Millisecond
	t.Cleanup(func() { clientWriteTimeout = old })

	s := &Server{}
	srv, cli := net.Pipe()
	defer cli.Close()
	c := &clientConn{conn: srv}
	s.client = c

	w := &clientSink{s: s}
	t0 := time.Now()
	if n, err := w.Write([]byte("x")); err == nil || n != 0 {
		t.Fatalf("failed frame must report its write failure: n=%d err=%v", n, err)
	}
	if took := time.Since(t0); took > 5*time.Second {
		t.Fatalf("wedged write took %v; deadline never fired", took)
	}
	s.mu.Lock()
	dropped := s.client == nil
	s.mu.Unlock()
	if !dropped {
		t.Fatal("wedged client was not dropped")
	}
}
