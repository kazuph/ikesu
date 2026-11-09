package server

import (
	"bufio"
	"fmt"
	"strings"
	"testing"
	"time"
)

// postTestServer returns a running-app Server with one attached client whose
// peer end is returned for the test to read (or not read).
func postTestServer(t *testing.T) (*Server, *clientConn, func(), interface {
	Read([]byte) (int, error)
	SetReadDeadline(time.Time) error
}) {
	t.Helper()
	s := newTestServer(t, "post")
	local, peer := writerSocketPair(t)
	c := &clientConn{conn: local}
	c.onWriteFail = s.dropConn
	s.clients = map[*clientConn]bool{c: true}
	s.client = c
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	stop := func() { s.app.Stop(); <-appDone }
	t.Cleanup(func() { c.close() })
	return s, c, stop, peer
}

// A client that stopped reading must not hold the app loop: sending from the
// app loop only queues.
func TestPostFromAppLoopDoesNotBlockOnStalledClient(t *testing.T) {
	old := clientWriteTimeout
	clientWriteTimeout = time.Second
	t.Cleanup(func() { clientWriteTimeout = old })
	s, c, stop, _ := postTestServer(t)
	defer stop()
	payload := []byte(strings.Repeat("x", 256<<10))
	s.app.Do(func() {
		sink := &clientSink{s: s}
		for i := 0; i < 20; i++ {
			_, _ = sink.Write(payload)
		}
	})
	s.app.Do(func() {})
	// Both calls returned while the client still holds unread data. The
	// blocking version only returned after the write deadline had dropped it.
	s.mu.Lock()
	attached := s.clients[c]
	s.mu.Unlock()
	if !attached {
		t.Fatal("app loop only returned after the stalled client was dropped")
	}
}

// post and send share one queue, so a reading client sees them in call order.
func TestPostAndSendKeepOrder(t *testing.T) {
	_, c, stop, peer := postTestServer(t)
	defer stop()
	const batches, perBatch = 40, 5
	got := make(chan []string, 1)
	go func() {
		rd := bufio.NewReader(&readerFunc{peer})
		var order []string
		for len(order) < batches*(perBatch+1) {
			_ = peer.SetReadDeadline(time.Now().Add(5 * time.Second))
			m, err := readClientMsg(rd)
			if err != nil {
				break
			}
			order = append(order, m.Reason+m.Data)
		}
		got <- order
	}()
	var want []string
	n := 0
	for b := 0; b < batches; b++ {
		for i := 0; i < perBatch; i++ {
			// Alternate the render and control paths of the writer.
			if n%2 == 0 {
				c.post(clientMsg{Type: "output", Data: fmt.Sprint(n)})
				want = append(want, fmt.Sprint(n))
			} else {
				c.post(clientMsg{Type: "error", Reason: fmt.Sprint(n)})
				want = append(want, fmt.Sprint(n))
			}
			n++
		}
		if err := c.send(clientMsg{Type: "error", Reason: fmt.Sprint(n)}); err != nil {
			t.Fatal(err)
		}
		want = append(want, fmt.Sprint(n))
		n++
	}
	order := <-got
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order differs\n got %v\nwant %v", order, want)
	}
}

type readerFunc struct {
	r interface{ Read([]byte) (int, error) }
}

func (r *readerFunc) Read(p []byte) (int, error) { return r.r.Read(p) }

// A client that never reads is detached after the write deadline, and its
// queue goroutine ends.
func TestPostStalledClientIsDroppedAndPumpEnds(t *testing.T) {
	old := clientWriteTimeout
	clientWriteTimeout = 300 * time.Millisecond
	t.Cleanup(func() { clientWriteTimeout = old })
	s, c, stop, _ := postTestServer(t)
	defer stop()
	payload := []byte(strings.Repeat("x", 256<<10))
	for i := 0; i < 10; i++ {
		c.post(clientMsg{Type: "output", Data: encode64(payload)})
	}
	select {
	case <-c.pumpDone:
	case <-time.After(5 * time.Second):
		t.Fatal("queue goroutine did not end after the write deadline")
	}
	s.mu.Lock()
	attached := s.clients[c] || s.client == c
	s.mu.Unlock()
	if attached {
		t.Fatal("stalled client stayed attached")
	}
	if err := c.send(clientMsg{Type: "exit"}); err == nil {
		t.Fatal("send after drop should fail")
	}
}
