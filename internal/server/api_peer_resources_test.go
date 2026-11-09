package server

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
)

func TestAPIPeerMonitorRecoversAfterRealEMFILE(t *testing.T) {
	if os.Getenv("PEER_INIT_TEST_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPIPeerMonitorRecoversAfterRealEMFILE$", "-test.v")
		cmd.Env = append(os.Environ(), "PEER_INIT_TEST_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	dir, err := os.MkdirTemp("/tmp", "ikesu-peer-emfile-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "peer.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	limited := limit
	limited.Cur = min(limit.Cur, 64)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limited); err != nil {
		t.Fatal(err)
	}
	defer unix.Setrlimit(unix.RLIMIT_NOFILE, &limit)
	var held []int
	for {
		fd, err := unix.Open(os.DevNull, unix.O_RDONLY, 0)
		if errors.Is(err, unix.EMFILE) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, fd)
	}
	if len(held) == 0 {
		t.Fatal("no descriptors available to release")
	}
	unix.Close(held[len(held)-1])
	held = held[:len(held)-1]
	raw, err := server.(syscall.Conn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	probe := -1
	var dupErr error
	if err := raw.Control(func(fd uintptr) { probe, dupErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		t.Fatal(err)
	}
	if dupErr != nil {
		t.Fatalf("socket dup did not succeed: %v", dupErr)
	}
	pipe := []int{0, 0}
	pipeErr := unix.Pipe(pipe)
	unix.Close(probe)
	if pipeErr == nil {
		unix.Close(pipe[0])
		unix.Close(pipe[1])
		t.Fatal("pipe unexpectedly succeeded")
	}
	if !errors.Is(pipeErr, unix.EMFILE) {
		t.Fatalf("pipe did not fail with EMFILE: %v", pipeErr)
	}
	_, stop, initErr := peerContext(server)
	for _, fd := range held {
		unix.Close(fd)
	}
	if initErr == nil {
		stop()
		t.Fatal("monitor unexpectedly initialized without descriptors")
	}
	if !errors.Is(initErr, unix.EMFILE) {
		t.Fatalf("expected real EMFILE, got %v", initErr)
	}
	ctx, stop, err := peerContext(server)
	if err != nil {
		t.Fatalf("monitor did not recover after descriptors were released: %v", err)
	}
	defer stop()
	s := newTestServer(t, "peer-recovery")
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	s.apiWG.Add(1)
	go s.serveAPI(server)
	if err := api.WriteRequest(client, api.Request{ID: "recovery", Method: api.MethodPaneList, Params: api.MarshalParams(api.PaneListParams{})}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response, err := api.ReadResponse(bufio.NewReader(client))
	if err != nil || response.Err != nil {
		t.Fatalf("API request after recovery: %+v %v", response, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("recovered monitor did not detect disconnect")
	}
	s.apiWG.Wait()
}

func TestAPIPeerMonitoringSharesPoller(t *testing.T) {
	if os.Getenv("PEER_RESOURCE_TEST_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPIPeerMonitoringSharesPoller$", "-test.v")
		cmd.Env = append(os.Environ(), "PEER_RESOURCE_TEST_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "peer.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connect := func() (net.Conn, net.Conn) {
		t.Helper()
		client, err := net.Dial("unix", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		server, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		return client, server
	}
	client, server := connect()
	_, stop, err := peerContext(server)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	client.Close()
	server.Close()
	const connections = 8
	clients, servers := make([]net.Conn, 0, connections), make([]net.Conn, 0, connections)
	for i := 0; i < connections; i++ {
		c, s := connect()
		clients = append(clients, c)
		servers = append(servers, s)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
		for _, s := range servers {
			s.Close()
		}
	}()
	fdCount := func() int {
		var limit unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		count := 0
		for fd := 0; fd < int(limit.Cur); fd++ {
			if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
				count++
			}
		}
		return count
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	threadsBefore, fdsBefore := pprof.Lookup("threadcreate").Count(), fdCount()
	stops := make([]func(), 0, connections)
	for _, c := range servers {
		_, stop, err := peerContext(c)
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}
	fdsDuring := fdCount()
	runtime.ReadMemStats(&after)
	threadsDuring := pprof.Lookup("threadcreate").Count()
	for _, stop := range stops {
		stop()
	}
	fdsAfter := fdCount()
	t.Logf("FD %d -> %d -> %d; threads %d -> %d; allocated %d -> %d bytes", fdsBefore, fdsDuring, fdsAfter, threadsBefore, threadsDuring, before.Alloc, after.Alloc)
	if fdsDuring-fdsBefore > connections {
		t.Errorf("%d monitoring FDs for %d connections; want at most one per connection after poller initialization", fdsDuring-fdsBefore, connections)
	}
	if fdsAfter != fdsBefore {
		t.Errorf("monitor FDs after stop = %d, want %d", fdsAfter, fdsBefore)
	}
}
