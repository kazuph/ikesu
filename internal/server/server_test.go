package server

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// testConn is a raw client-socket connection for tests.
type testConn struct {
	conn        net.Conn
	rd          *bufio.Reader
	pending     []byte
	readClosed  bool
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
		conn, err := net.DialTimeout("unix", api.ClientSocketPath(dir), time.Until(deadline))
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
	got, err := c.readUntilResult(timeout, pred)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (c *testConn) readUntilResult(timeout time.Duration, pred func(clientMsg) bool) ([]clientMsg, error) {
	var got []clientMsg
	var ackErr error
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		part, err := c.rd.ReadBytes('\n')
		c.pending = append(c.pending, part...)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			c.readClosed = true
			if ackErr != nil {
				return got, fmt.Errorf("render acknowledgement failed before terminal message: %w", ackErr)
			}
			return got, nil // EOF
		}
		line := c.pending
		c.pending = nil
		var m clientMsg
		if json.Unmarshal(line, &m) == nil {
			if m.Type == "snapshot" {
				data, err := decode64(m.Data)
				if err != nil {
					return got, err
				}
				in, err := c.snapshots.Decode(data)
				if err != nil {
					return got, err
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
						return got, fmt.Errorf("render acknowledgement: %w", ackErr)
					}
					c.lastRegions = out
				}
			}
			got = append(got, m)
			if pred(m) {
				// A closed write half can coexist with a queued restart/exit message.
				// Require that real terminal message; never hide ACK errors on live reads.
				if ackErr != nil && m.Type != "restart" && m.Type != "exit" && m.Type != "detached" {
					return got, fmt.Errorf("render acknowledgement failed on live read: %w", ackErr)
				}
				return got, nil
			}
		}
	}
	if ackErr != nil {
		return got, fmt.Errorf("render acknowledgement failed without terminal message: %w", ackErr)
	}
	return got, nil
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
	base, err := os.MkdirTemp("/tmp", "ikesutest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
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
	waitAPISocket(t, dir)
	return dir, done
}

func TestAttachDetachReattach(t *testing.T) {
	teststate.Lock(t)
	useTestShellPrompt(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)

	// Attach 1: hello, then drive the pane.
	c1 := dialClient(t, dir)
	c1.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	msgs := c1.readUntil(t, 10*time.Second, func(m clientMsg) bool {
		return m.Type == "output"
	})
	if outputs(msgs) == "" {
		t.Fatal("no output frames received")
	}
	waitRead(t, dir, firstPaneID(t, dir), "ikesu-test-ready")

	// Type a command; the echo output should arrive over the socket.
	c1.send(t, clientMsg{Type: "input", Data: encode64([]byte("echo ikesumarker42\n"))})
	var sawMarker bool
	for i := 0; i < 20 && !sawMarker; i++ {
		msgs = c1.readUntil(t, time.Second, func(m clientMsg) bool {
			b, _ := base64.StdEncoding.DecodeString(m.Data)
			return m.Type == "output" && strings.Contains(string(b), "ikesumarker42")
		})
		sawMarker = strings.Contains(outputs(msgs), "ikesumarker42")
	}
	if !sawMarker {
		t.Fatal("pane output never echoed ikesumarker42")
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
		return m.Type == "output" && strings.Contains(string(b), "ikesumarker42")
	})
	if !strings.Contains(outputs(msgs), "ikesumarker42") {
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
	if _, err := os.Stat(filepath.Join(dir, "ikesu.sock")); !os.IsNotExist(err) {
		t.Error("ikesu.sock not removed on shutdown")
	}
}

func TestSecondClientPreservesFirst(t *testing.T) {
	teststate.Lock(t)
	useTestShellPrompt(t)
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
	waitRead(t, dir, pid, "ikesu-test-ready")
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

func TestServerRecreatesHomeWhenLastPaneExits(t *testing.T) {
	teststate.Lock(t)
	useTestShellPrompt(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	clients := []*testConn{dialClient(t, dir), dialClient(t, dir)}
	for _, c := range clients {
		defer c.conn.Close()
		c.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	}
	old := firstPaneID(t, dir)
	waitRead(t, dir, old, "ikesu-test-ready")
	clients[0].send(t, clientMsg{Type: "input", Data: encode64([]byte("exit\n"))})
	for _, c := range clients {
		messages := c.readUntil(t, 10*time.Second, func(m clientMsg) bool {
			in := c.lastInput
			return m.Type == "output" && in != nil && in.State != nil && len(in.State.Workspaces) == 1 && in.State.Workspaces[0].ID != 1 && in.State.Workspaces[0].Cwd == home
		})
		for _, m := range messages {
			if m.Type == "exit" {
				t.Fatal("shell exit terminated a connected client")
			}
		}
		if c.lastInput == nil || c.lastInput.State == nil || len(c.lastInput.State.Workspaces) != 1 || c.lastInput.State.Workspaces[0].ID == 1 || c.lastInput.State.Workspaces[0].Cwd != home {
			t.Fatal("client did not display a fresh home workspace")
		}
	}
	next := firstPaneID(t, dir)
	if next == old {
		t.Fatal("last pane identity was reused")
	}
	for n, c := range clients {
		marker := fmt.Sprintf("HOME_CLIENT_%d_ALIVE", n)
		c.send(t, clientMsg{Type: "input", Data: encode64([]byte(fmt.Sprintf("printf 'HOME_CLIENT_%%s_ALIVE\\n' %d\n", n)))})
		waitRead(t, dir, next, marker)
	}
	select {
	case <-done:
		t.Fatal("server exited after replacing last shell")
	default:
	}
	// Real clients drain independently. Start both readers before stopping so
	// one unread snapshot cannot block the other client's exit notification.
	var ready sync.WaitGroup
	ready.Add(len(clients))
	type readResult struct {
		messages []clientMsg
		err      error
	}
	results := make(chan readResult, len(clients))
	for _, c := range clients {
		go func(c *testConn) {
			ready.Done()
			messages, err := c.readUntilResult(10*time.Second, func(m clientMsg) bool { return m.Type == "exit" })
			results <- readResult{messages, err}
		}(c)
	}
	ready.Wait()
	apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil)
	completed := make([]readResult, 0, len(clients))
	for range clients {
		completed = append(completed, <-results)
	}
	for _, result := range completed {
		if result.err != nil {
			t.Fatal(result.err)
		}
		exited := false
		for _, m := range result.messages {
			exited = exited || m.Type == "exit"
		}
		if !exited {
			t.Fatal("explicit server.stop did not send exit")
		}
	}
	waitServerExit(t, done)
}

// A real login shell reads this after the system profile. macOS's /etc/bashrc
// overwrites an inherited PS1, so an environment-only readiness marker is lost.
func useTestShellPrompt(t *testing.T) {
	t.Helper()
	home := teststate.TempDir(t)
	teststate.Setenv(t, "HOME", home)
	// Linux's non-login shell inherits PS1; macOS resets it before .profile.
	teststate.Setenv(t, "PS1", "ikesu-test-ready$ ")
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("PS1='ikesu-test-ready$ '\nexport PS1\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func stopServer(t *testing.T, dir string, done chan int) {
	t.Helper()
	conn, err := net.DialTimeout("unix", api.APISocketPath(dir), 10*time.Second)
	if err != nil {
		return
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	json.NewEncoder(conn).Encode(map[string]any{"id": "t", "method": "server.stop", "params": map[string]any{}})
	bufio.NewReader(conn).ReadBytes('\n')
	conn.Close()
	waitServerExit(t, done)
}

func waitServerExit[T any](t *testing.T, done <-chan T) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit before fixture deadline")
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
	base, err := os.MkdirTemp("/tmp", "ikesutest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	teststate.Setenv(t, "IKESU_STATE_DIR", base)
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
