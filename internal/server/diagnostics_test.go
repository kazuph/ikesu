package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestDiagnosticsDuringBlockedLoop(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics")
	s.dir = teststate.TempDir(t)
	path := filepath.Join(s.dir, "ikesu.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s.apiLn = listener
	teststate.Setenv(t, "IKESU_SOCKET_PATH", path)
	teststate.Setenv(t, "IKESU_SOCKET_PATH_EXPLICIT", "1")
	appDone := make(chan struct{})
	apiDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	go func() { s.apiLoop(); close(apiDone) }()
	go func() { s.watchAppLoop(appDone); close(watchDone) }()
	entered := make(chan struct{})
	release := make(chan struct{})
	ended := make(chan struct{})
	go func() { s.app.Do(func() { close(entered); <-release }); close(ended) }()
	<-entered
	defer func() {
		close(release)
		<-ended
		s.app.Stop()
		<-appDone
		<-watchDone
		listener.Close()
		<-apiDone
		s.apiWG.Wait()
		s.sup.KillAll()
	}()
	call := func(args ...string) (string, string, int) {
		var out, errout bytes.Buffer
		code := cli.Run(args, cli.Options{Stdout: &out, Stderr: &errout})
		return out.String(), errout.String(), code
	}
	out, errout, code := call("server", "profile", "--cpu", "100ms", "--out", "cpu.pprof")
	if code != 0 {
		t.Fatalf("profile: %d %s", code, errout)
	}
	if info, err := os.Stat(filepath.Join(s.dir, "cpu.pprof")); err != nil || info.Size() == 0 {
		t.Fatalf("profile missing: %v", err)
	}
	out, errout, code = call("server", "stacks", "--out", "manual.stack")
	if code != 0 {
		t.Fatalf("stack: %d %s", code, errout)
	}
	if !strings.Contains(out, "manual.stack") {
		t.Fatal(out)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "manual.stack"))
	if err != nil || !bytes.Contains(data, []byte("goroutine ")) {
		t.Fatalf("stack: %v %s", err, data)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "goroutine ") && !strings.HasPrefix(line, "created by ") && strings.Contains(line, "0x") {
			t.Fatalf("goroutine arguments were not redacted: %s", line)
		}
	}
	if _, errout, code = call("server", "stacks", "--out", filepath.Join(s.dir, "absolute.stack")); code != 0 {
		t.Fatalf("absolute state path rejected: %s", errout)
	}
	if _, errout, code = call("server", "stacks", "--out", "../bad"); code == 0 {
		t.Fatalf("accepted traversal: %s", errout)
	}
	if _, errout, code = call("server", "stacks", "--out", "manual.stack"); code == 0 {
		t.Fatalf("overwrote file: %s", errout)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && s.stallStatus().Count == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	out, errout, code = call("status", "--json")
	if code != 0 {
		t.Fatal(errout)
	}
	var status struct {
		Server struct {
			AppLoopStall api.LoopStallStatus `json:"app_loop_stall"`
		} `json:"server"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if status.Server.AppLoopStall.Count != 1 || status.Server.AppLoopStall.LongestMillis < 2000 {
		t.Fatalf("stall status: %s", out)
	}
	var matches []string
	for time.Now().Before(deadline) {
		matches, err = filepath.Glob(filepath.Join(s.dir, "app-loop-stalls", "*.stack"))
		if len(matches) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(matches) != 1 {
		t.Fatalf("automatic stack: %v %v", matches, err)
	}
}

func TestDiagnosticsRejectsNonUnixPeer(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-transport")
	s.dir = teststate.TempDir(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.apiLn = listener
	done := make(chan struct{})
	go func() { s.apiLoop(); close(done) }()
	defer func() { listener.Close(); <-done; s.apiWG.Wait(); s.sup.KillAll() }()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := api.Request{ID: "test", Method: api.MethodServerStacks, Params: api.MarshalParams(api.DiagnosticsParams{Name: "forbidden.stack"})}
	if err := api.WriteRequest(conn, req); err != nil {
		t.Fatal(err)
	}
	response, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if response.Err == nil || response.Err.Code != "permission_denied" {
		t.Fatalf("unauthorized response: %+v", response)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "forbidden.stack")); !os.IsNotExist(err) {
		t.Fatalf("unauthorized file: %v", err)
	}
}

// The wait exercises the real two-second stall threshold.
func TestRecoveredLoopRetainsStallStatistics(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-recovered")
	s.dir = teststate.TempDir(t)
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone; s.sup.KillAll() }()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() { s.app.Do(func() { close(entered); <-release }); close(finished) }()
	<-entered
	time.Sleep(stallThreshold + 20*time.Millisecond)
	close(release)
	<-finished
	// An additional job cannot complete before the loop records its next iteration.
	s.app.Do(func() {})
	got := s.stallStatus()
	if got.Count != 1 || got.LongestMillis < stallThreshold.Milliseconds() || got.CurrentMillis != 0 {
		t.Fatalf("recovered stall: %+v", got)
	}
}

// The real stall duration is required to exercise status during recovery.
func TestStallCountDoesNotDecreaseDuringRecovery(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-recovery-concurrent")
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done; s.sup.KillAll() }()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { s.app.Do(func() { close(entered); <-release }); close(finished) }()
	<-entered
	time.Sleep(stallThreshold + 20*time.Millisecond)
	stop := make(chan struct{})
	results := make(chan string, 16)
	var readers sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			var previous uint64
			for {
				select {
				case <-stop:
					return
				default:
				}
				count := s.stallStatus().Count
				if count > 1 || count < previous {
					results <- fmt.Sprintf("count changed from %d to %d", previous, count)
					return
				}
				previous = count
			}
		}()
	}
	close(release)
	<-finished
	s.app.Do(func() {})
	close(stop)
	readers.Wait()
	close(results)
	for result := range results {
		t.Error(result)
	}
	if got := s.stallStatus().Count; got != 1 {
		t.Fatalf("final count=%d", got)
	}
}

// Two real two-second stalls are needed to exercise the one-minute cooldown.
func TestStallStackCooldown(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-cooldown")
	s.dir = teststate.TempDir(t)
	appDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	go func() { s.watchAppLoop(appDone); close(watcherDone) }()
	defer func() { s.app.Stop(); <-appDone; <-watcherDone; s.sup.KillAll() }()
	for i := 0; i < 2; i++ {
		entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() { s.app.Do(func() { close(entered); <-release }); close(finished) }()
		<-entered
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && s.stallStatus().Count < uint64(i+1) {
			time.Sleep(10 * time.Millisecond)
		}
		got := s.stallStatus().Count
		firstSaved := i != 0
		if i == 0 {
			for time.Now().Before(deadline) {
				files, _ := filepath.Glob(filepath.Join(s.dir, "app-loop-stalls", "*.stack"))
				if len(files) == 1 {
					firstSaved = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		close(release)
		<-finished
		s.app.Do(func() {})
		if got != uint64(i+1) {
			t.Fatalf("stall %d was not observed: %d", i+1, got)
		}
		if !firstSaved {
			t.Fatal("first stall did not save a stack")
		}
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "app-loop-stalls", "*.stack"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cooldown did not retain exactly one stack: %v %v", files, err)
	}
}

func TestStallStackRetention(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-retention")
	s.dir = teststate.TempDir(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < stallStackLimit+1; i++ {
		s.saveStallStack(start.Add(time.Duration(i) * time.Minute))
	}
	matches, err := filepath.Glob(filepath.Join(s.dir, "app-loop-stalls", "*.stack"))
	if err != nil || len(matches) != stallStackLimit {
		t.Fatalf("retained %d stacks: %v", len(matches), err)
	}
	old := filepath.Join(s.dir, "app-loop-stalls", start.Format("20060102T150405.000000000Z")+".stack")
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("oldest stack remains: %v", err)
	}
	s.sup.KillAll()
}

func TestStallStackRejectsSymlinkDirectory(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-symlink")
	s.dir = teststate.TempDir(t)
	outside := t.TempDir()
	for i := 0; i <= stallStackLimit; i++ {
		name := time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC).Format("20060102T150405.000000000Z") + ".stack"
		if err := os.WriteFile(filepath.Join(outside, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(s.dir, "app-loop-stalls")); err != nil {
		t.Fatal(err)
	}
	s.saveStallStack(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != stallStackLimit+1 {
		t.Fatalf("outside directory changed: entries=%d err=%v", len(entries), err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(outside, entry.Name()))
		if err != nil || string(data) != "keep" {
			t.Fatalf("outside file %s changed: %v", entry.Name(), err)
		}
	}
	s.sup.KillAll()
}

func TestStallRetentionPreservesUnrelatedStack(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "diagnostics-unrelated")
	s.dir = teststate.TempDir(t)
	dir := filepath.Join(s.dir, "app-loop-stalls")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "0000-unrelated.stack")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= stallStackLimit; i++ {
		s.saveStallStack(start.Add(time.Duration(i) * time.Minute))
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated stack changed: %v", err)
	}
	s.sup.KillAll()
}

//go:noinline
func diagnosticCaptureMarker(ready chan<- struct{}, release <-chan struct{}) {
	ready <- struct{}{}
	<-release
}

func TestStacksContainAllWaitingGoroutines(t *testing.T) {
	const goroutines = 8
	ready := make(chan struct{}, goroutines)
	release := make(chan struct{})
	defer close(release)
	for i := 0; i < goroutines; i++ {
		go diagnosticCaptureMarker(ready, release)
	}
	for i := 0; i < goroutines; i++ {
		<-ready
	}
	f, err := os.CreateTemp(t.TempDir(), "stacks-")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStacks(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(data, []byte("diagnosticCaptureMarker(")); count != goroutines {
		t.Fatalf("waiting goroutines in stack: got %d want %d", count, goroutines)
	}
}
