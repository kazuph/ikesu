package latency

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestPeerPIDRealSockets(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gtxpeer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for _, network := range []string{"unix", "tcp"} {
		t.Run(network, func(t *testing.T) {
			address := filepath.Join(dir, "socket")
			want := os.Getpid()
			if network == "tcp" {
				address = "127.0.0.1:0"
				want = 0
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			client, err := net.Dial(network, listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			server, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			for _, conn := range []net.Conn{client, server} {
				if got := PeerPID(conn); got != want {
					t.Fatalf("peer PID=%d want=%d", got, want)
				}
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			if got := PeerPID(client); got != 0 {
				t.Fatalf("closed connection PID=%d", got)
			}
		})
	}
}
