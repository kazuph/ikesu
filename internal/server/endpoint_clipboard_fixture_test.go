package server

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Use real PTYs, app-loop delivery, and Unix API/display sockets. The observer
// runs after production clipboard delivery, including when no viewer sees it.
func startClipboardEndpoint(t *testing.T) (*Server, <-chan string) {
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
	processed := make(chan string, 1)
	s.app.SetClipboardHook(func(pid int, data string) {
		s.deliverPaneClipboard(pid, data)
		processed <- data
	})
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
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
