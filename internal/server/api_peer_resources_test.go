package server

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"

	"golang.org/x/sys/unix"
)

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
