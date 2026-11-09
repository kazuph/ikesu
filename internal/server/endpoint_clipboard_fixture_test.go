package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/client"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Use real PTYs, app-loop delivery, and Unix API/display sockets. The observer
// runs after production clipboard delivery, including when no viewer sees it.
func startClipboardEndpoint(t *testing.T) (*Server, <-chan string) {
	return startClipboardEndpointImported(t, nil, nil)
}

func startClipboardEndpointImported(t *testing.T, states []runtime.PaneTransfer, files []*os.File) (*Server, <-chan string) {
	t.Helper()
	pinTestConfig(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := newTestServer(t, "clipboard")
	dir, err := os.MkdirTemp("/tmp", "ikesu-clip-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s.dir = dir
	teststate.Setenv(t, "IKESU_STATE_DIR", filepath.Dir(dir))
	s.session = filepath.Base(dir)
	s.endpointPeer = endpoint.Current()
	s.cfg, err = loadTestConfig(t)
	if err != nil {
		t.Fatal(err)
	}
	s.apiLn, err = net.Listen("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.apiLn.Close() })
	s.clientLn, err = net.Listen("unix", api.ClientSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.clientLn.Close() })
	s.app.SetServerUI()
	s.app.SetViewerHooks(s.drawViewers, s.tickViewers, s.visiblePane)
	s.app.SetSidebarConfig(s.cfg, true)
	processed := make(chan string, 1024)
	s.app.SetClipboardHook(func(pid int, data string, transferred bool) {
		s.deliverPaneClipboard(pid, data, transferred)
		processed <- data
	})
	if states == nil {
		if err := s.app.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := s.sup.ImportHandoff(states, files); err != nil {
			t.Fatal(err)
		}
		s.sup.StartImported()
	}
	appDone, apiDone, clientDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	go func() { s.apiLoop(); close(apiDone) }()
	go func() { s.clientLoop(); close(clientDone) }()
	t.Cleanup(func() {
		s.app.Stop()
		<-appDone
		s.apiLn.Close()
		s.clientLn.Close()
		<-apiDone
		<-clientDone
		s.apiWG.Wait()
		s.sup.KillAll()
	})
	return s, processed
}

func TestClipboardMarkerPrecedesAppDelivery(t *testing.T) {
	teststate.Lock(t)
	s, processed := startClipboardEndpoint(t)
	const marker = "CLIPBOARD_RUNTIME_MARKER"
	want := "\x1b]52;c;" + encode64([]byte("PENDING")) + "\a"
	// Hold the real app loop while its real PTY/emulator continues processing.
	var observationErr error
	s.app.Do(func() {
		observationErr = func() error {
			pane := s.sup.Get(s.app.State().ActiveWorkspace().TabRef().Layout.Focused())
			command := fmt.Sprintf("printf '\\033]52;c;%s\\007'; printf 'CLIPBOARD_RUNTIME_%%s\\n' MARKER\n", encode64([]byte("PENDING")))
			if _, err := pane.WriteToPTY([]byte(command)); err != nil {
				return err
			}
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				lines, err := pane.ReadLines("recent", true)
				if err != nil {
					return err
				}
				// The marker is split in the command to exclude terminal input echo.
				if strings.Contains(strings.Join(lines, ""), marker) {
					break
				}
				select {
				case <-ticker.C:
				case <-deadline.C:
					return fmt.Errorf("real PTY marker missing")
				}
			}
			select {
			case <-processed:
				return fmt.Errorf("delivery completed while app loop was held")
			default:
			}
			return nil
		}()
	})
	if observationErr != nil {
		t.Fatal(observationErr)
	}
	select {
	case got := <-processed:
		if got != want {
			t.Fatalf("clipboard=%q want=%q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not complete after releasing app loop")
	}
	t.Log("real emulator marker was readable before OSC52 delivery; explicit delivery observation completed after app release")
}

func TestTransferredClipboardWaitsForRealDisplay(t *testing.T) {
	teststate.Lock(t)
	old := runtime.NewSupervisor()
	defer old.KillAll()
	pane, err := old.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", "read line; printf '\\033]52;c;UkVBRFk=\\007\\033]52;c;T05F\\007\\033]52;c;VFdP\\007'; read line"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pane.WriteToPTY([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	// Seeing the first runtime event establishes that the real PTY has produced output.
	first := <-old.Events()
	if first.Kind != runtime.EventClipboard {
		for first.Kind != runtime.EventClipboard {
			first = <-old.Events()
		}
	}
	// Pause captures any events already queued and those held by the pump.
	states, files, err := old.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || len(states[0].Clipboard) < 2 {
		t.Fatal("no transferred clipboard values")
	}
	want := strings.Join(states[0].Clipboard, "")
	s, processed := startClipboardEndpointImported(t, states, files)
	old.ReleaseHandoff()
	for range states[0].Clipboard {
		<-processed
	}
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	clientDone := make(chan error, 1)
	go func() { _, err := client.Attach(s.session, terminal, terminal, "test"); clientDone <- err }()
	defer func() { master.Close(); <-clientDone }()
	var got strings.Builder
	buf := make([]byte, 4096)
	for !strings.Contains(got.String(), "\x1b[?2026l") {
		n, err := master.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if err != nil {
			t.Fatalf("display clipboard = %q, want transferred %q: %v", got.String(), want, err)
		}
	}
	if !strings.Contains(got.String(), want) {
		t.Fatalf("display clipboard = %q, want transferred %q", got.String(), want)
	}
	for _, value := range states[0].Clipboard {
		if strings.Count(got.String(), value) != 1 {
			t.Fatalf("clipboard duplicated or missing: %q", got.String())
		}
	}
}
