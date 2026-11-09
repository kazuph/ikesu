package server

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestAcceptSurvivesRealEMFILE runs a child process (see TestMain) that exhausts its own
// descriptor limit while a client connects, then frees descriptors. The
// listener must accept the waiting connection afterwards instead of
// stopping for good (opus audit ⑥).
func TestAcceptSurvivesRealEMFILE(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "s.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestAcceptSurvivesRealEMFILE$")
	cmd.Env = append(os.Environ(), "IKESU_EMFILE_CHILD="+sock)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	lines := bufio.NewScanner(stdout)
	expect := func(want string) {
		t.Helper()
		done := make(chan bool, 1)
		go func() {
			for lines.Scan() {
				if lines.Text() == want {
					done <- true
					return
				}
			}
			done <- false
		}()
		select {
		case ok := <-done:
			if !ok {
				t.Fatalf("child ended before %q", want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("child never reported %q", want)
		}
	}
	expect("full")
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expect("emfile")
	fmt.Fprintln(stdin, "release")
	expect("released")
	// The kernel drops the connection that hit EMFILE; a client's next
	// connection must be accepted by the same, still running loop.
	again, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	expect("accepted")
}

func emfileChild() {
	sock := os.Getenv("IKESU_EMFILE_CHILD")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Println("listen", err)
		os.Exit(1)
	}
	limit := syscall.Rlimit{Cur: 64, Max: 64}
	var old syscall.Rlimit
	_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &old)
	limit.Max = old.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		fmt.Println("setrlimit", err)
		os.Exit(1)
	}
	var held []*os.File
	for {
		f, err := os.Open(os.DevNull)
		if err != nil {
			break
		}
		held = append(held, f)
	}
	fmt.Println("full")
	sawEMFILE := make(chan struct{}, 1)
	probe := &observingListener{UnixListener: ln.(*net.UnixListener), emfile: sawEMFILE}
	accepted := make(chan error, 1)
	go func() {
		c, err := acceptWithRetry(probe, func() bool { return false })
		if c != nil {
			c.Close()
		}
		accepted <- err
	}()
	<-sawEMFILE
	fmt.Println("emfile")
	bufio.NewScanner(os.Stdin).Scan()
	for _, f := range held {
		f.Close()
	}
	fmt.Println("released")
	if err := <-accepted; err != nil {
		fmt.Println("accept", err)
		os.Exit(1)
	}
	fmt.Println("accepted")
	os.Exit(0)
}

// observingListener reports the real EMFILE from the kernel; it does not
// create errors of its own.
type observingListener struct {
	*net.UnixListener
	emfile chan struct{}
}

func (l *observingListener) Accept() (net.Conn, error) {
	c, err := l.UnixListener.Accept()
	if errors.Is(err, syscall.EMFILE) {
		select {
		case l.emfile <- struct{}{}:
		default:
		}
	}
	return c, err
}
