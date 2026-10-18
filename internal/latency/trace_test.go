package latency

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceLimitWritesNoticeOnce(t *testing.T) {
	old := Enabled
	Enabled = true
	defer func() { Enabled = old }()
	var out bytes.Buffer
	tr := New(&out)
	tr.bytes = Limit - 1
	tr.Event("over", "payload")
	tr.Event("discard", "payload")
	tr.Close()
	if out.String() != LimitMessage {
		t.Fatalf("limit output=%q", out.String())
	}
}

func TestTraceSlowRealPipeDoesNotBlockProducer(t *testing.T) {
	old := Enabled
	Enabled = true
	defer func() { Enabled = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	tr := New(w)
	for i := 0; i < 100; i++ {
		tr.Event("write", strings.Repeat("x", 4096))
	}
	// Pipe capacity is deliberately exceeded before a reader starts. All
	// runtime-state notifications can still run while the log writer is stuck.
	state := make(chan struct{})
	close(state)
	<-state
	drained := make(chan string)
	go func() { data, _ := io.ReadAll(r); drained <- string(data) }()
	tr.Close()
	w.Close()
	data := <-drained
	if strings.Count(data, "event=write") != 100 {
		t.Fatal("lost pipe records")
	}
}

func TestTraceClientFileDisabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	path := filepath.Join(t.TempDir(), "client.log")
	if Open(path) != nil {
		t.Fatal("disabled logger created")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("disabled file exists", err)
	}
	var nilTrace *Trace
	nilTrace.Event("ignored")
}

func TestTraceStartupEnvironmentAndExistingBytes(t *testing.T) {
	if os.Getenv("GTX_TRACE_TEST_CHILD") == "1" {
		path := os.Getenv("GTX_TRACE_TEST_PATH")
		trace := Open(path)
		trace.Event("startup")
		trace.Close()
		return
	}
	for _, value := range []string{"unset", "0", "1", "01", "true"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.log")
			cmd := exec.Command(os.Args[0], "-test.run=^TestTraceStartupEnvironmentAndExistingBytes$")
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "GTX_LATENCY_TRACE=") {
					cmd.Env = append(cmd.Env, item)
				}
			}
			cmd.Env = append(cmd.Env, "GTX_TRACE_TEST_CHILD=1", "GTX_TRACE_TEST_PATH="+path)
			if value != "unset" {
				cmd.Env = append(cmd.Env, "GTX_LATENCY_TRACE="+value)
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child %v %s", err, out)
			}
			data, err := os.ReadFile(path)
			if value == "1" {
				if err != nil || !bytes.Contains(data, []byte("event=startup")) {
					t.Fatal("enabled trace absent", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("disabled trace file exists", value, err)
			}
		})
	}
	old := Enabled
	Enabled = true
	defer func() { Enabled = old }()
	path := filepath.Join(t.TempDir(), "existing.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	trace := Open(path)
	trace.Event("fresh")
	if trace.bytes == 0 || trace.bytes >= Limit {
		t.Fatal("existing bytes counted")
	}
	trace.Close()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte("existing\n")) {
		t.Fatal("existing log replaced", err)
	}
}

func TestTraceWriteFailureDoesNotStopProducer(t *testing.T) {
	old := Enabled
	Enabled = true
	defer func() { Enabled = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	defer w.Close()
	tr := New(w)
	tr.Event("failed-write")
	tr.Event("still-running")
	tr.Close()
}

func TestTraceRealFileBudgetAndSingleLimitNotice(t *testing.T) {
	old := Enabled
	Enabled = true
	defer func() { Enabled = old }()
	path := filepath.Join(t.TempDir(), "server.log")
	prefix := []byte("existing ordinary server output\n")
	if err := os.WriteFile(path, prefix, 0600); err != nil {
		t.Fatal(err)
	}
	trace := Open(path)
	payload := strings.Repeat("x", 4096)
	for !trace.stopped {
		trace.Event("budget", payload)
	}
	trace.Event("must-be-dropped", "late")
	written := trace.bytes
	trace.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, prefix) || bytes.Count(data, []byte(LimitMessage)) != 1 || bytes.Contains(data, []byte("must-be-dropped")) {
		t.Fatal("budget changed ordinary log or emitted repeated/late records")
	}
	if written > Limit || len(data) != len(prefix)+written+len(LimitMessage) {
		t.Fatalf("budget accounting: file=%d existing=%d records=%d limit=%d", len(data), len(prefix), written, Limit)
	}
	if Limit-written > len(payload)+256 {
		t.Fatal("stopped far before the approved limit", written)
	}
}

func TestDisabledTraceDoesNotAllocate(t *testing.T) {
	var trace *Trace
	if n := testing.AllocsPerRun(100, func() { trace.Event("ignored", " revision=", uint64(3)) }); n != 0 {
		t.Fatal("disabled trace allocated", n)
	}
}
