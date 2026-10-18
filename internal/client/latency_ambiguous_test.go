package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/latency"
	"github.com/kazuph/goterrace/internal/render"
)

func TestTraceExpiredProbeCannotMatchLaterReply(t *testing.T) {
	for _, change := range []string{"same", "endpoint", "generation"} {
		t.Run(change, func(t *testing.T) {
			m, finish := traceProbeSession(t)
			m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now().Add(-2*render.TerminalStatusWait))
			m.tracePaint([]byte("first"), true, render.Input{SurfaceRevision: 1})
			m.traceProbeTimeout(time.Now())
			if change == "endpoint" {
				m.active = "second"
			}
			if change == "generation" {
				m.endpoints[m.active].attempt++
			}
			m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now())
			m.tracePaint([]byte("second"), true, render.Input{SurfaceRevision: 2})
			for reply := 0; reply < 2; reply++ {
				if err := m.input(input.Event{Raw: []byte("\x1b[0n")}); err != nil {
					t.Fatal(err)
				}
				if !m.outputFence.Deadline().IsZero() {
					t.Fatal("diagnostics changed functional reply handling")
				}
			}
			log := finish()
			if !strings.Contains(log, "draw_seq=1 reason=timeout") {
				t.Fatal("missing first probe timeout", log)
			}
			var replies int
			for _, line := range strings.Split(log, "\n") {
				if strings.Contains(line, "event=probe.reply ") {
					replies++
					if !strings.Contains(line, "draw_seq=0 matched=false") {
						t.Fatal("unnumbered reply incorrectly attributed to a draw", line)
					}
				}
			}
			if replies != 2 {
				t.Fatalf("reply records=%d want=2: %s", replies, log)
			}
		})
	}
}

func TestTraceSingleProbeStillMatchesReply(t *testing.T) {
	m, finish := traceProbeSession(t)
	m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now())
	m.tracePaint([]byte("single"), true, render.Input{SurfaceRevision: 1})
	if err := m.input(input.Event{Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatal(err)
	}
	if !m.outputFence.Deadline().IsZero() {
		t.Fatal("normal reply left functional wait")
	}
	if log := finish(); !strings.Contains(log, "draw_seq=1 matched=true") {
		t.Fatal("single probe lost its match", log)
	}
}

func traceProbeSession(t *testing.T) (*machineClient, func() string) {
	t.Helper()
	old := latency.Enabled
	latency.Enabled = true
	t.Cleanup(func() { latency.Enabled = old })
	path := filepath.Join(t.TempDir(), "trace.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := latency.New(file)
	t.Cleanup(func() { tr.Close(); file.Close() })
	m := &machineClient{trace: tr, active: "first", endpoints: map[string]*managedEndpoint{
		"first": {attempt: 1, boot: "first-boot"}, "second": {attempt: 2, boot: "second-boot"},
	}}
	return m, func() string {
		tr.Close()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}
