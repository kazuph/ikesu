package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/latency"
	"github.com/kazuph/ikesu/internal/render"
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
			assertProbeReplies(t, log, []string{"draw_seq=0 matched=false", "draw_seq=2 matched=true"})
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

func TestTraceMissingOldReplyRecoversOnFollowingQueries(t *testing.T) {
	m, finish := traceProbeSession(t)
	traceProbeWrite(m, true)
	traceProbeWrite(m, false)
	traceProbeInput(t, m)
	traceProbeWrite(m, false)
	traceProbeInput(t, m)
	traceProbeWrite(m, false)
	traceProbeInput(t, m)
	assertProbeReplies(t, finish(), []string{"draw_seq=0 matched=false", "draw_seq=3 matched=true", "draw_seq=4 matched=true"})
}

func TestTraceMultipleExpiredRepliesAreCountedOnce(t *testing.T) {
	m, finish := traceProbeSession(t)
	traceProbeWrite(m, true)
	m.traceProbeTimeout(time.Now())
	traceProbeWrite(m, true)
	m.traceProbeTimeout(time.Now())
	traceProbeWrite(m, false)
	traceProbeInput(t, m)
	traceProbeInput(t, m)
	traceProbeInput(t, m)
	assertProbeReplies(t, finish(), []string{"draw_seq=0 matched=false", "draw_seq=0 matched=false", "draw_seq=3 matched=true"})
}

func TestTraceProbeSwitchResetsOldCount(t *testing.T) {
	for _, change := range []string{"endpoint", "generation"} {
		for _, old := range []string{"none", "pending", "two_expired"} {
			t.Run(change+"/"+old, func(t *testing.T) {
				m, finish := traceProbeSession(t)
				switch old {
				case "pending":
					traceProbeWrite(m, false)
				case "two_expired":
					traceProbeWrite(m, true)
					traceProbeWrite(m, true)
				}
				if change == "endpoint" {
					m.active = "second"
				} else {
					m.endpoints[m.active].attempt++
				}
				traceProbeWrite(m, false)
				matched := fmt.Sprintf("draw_seq=%d matched=true", m.drawSeq)
				want := []string{matched}
				if old != "none" {
					traceProbeInput(t, m)
					want = append([]string{"draw_seq=0 matched=false"}, want...)
				}
				traceProbeInput(t, m)
				assertProbeReplies(t, finish(), want)
			})
		}
	}
}

func traceProbeWrite(m *machineClient, expired bool) {
	now := time.Now()
	if expired {
		now = now.Add(-2 * render.TerminalStatusWait)
	}
	m.outputFence.Written([]byte("\x1b_Ga=t,"), now)
	m.tracePaint([]byte("pixels"), true, render.Input{})
	if expired {
		m.traceProbeTimeout(time.Now())
	}
}

func traceProbeInput(t *testing.T, m *machineClient) {
	t.Helper()
	if err := m.input(input.Event{Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatal(err)
	}
	if !m.outputFence.Deadline().IsZero() {
		t.Fatal("diagnostics changed functional reply handling")
	}
}

func assertProbeReplies(t *testing.T, log string, want []string) {
	t.Helper()
	var replies []string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "event=probe.reply ") {
			replies = append(replies, line)
		}
	}
	if len(replies) != len(want) {
		t.Fatalf("reply count=%d want=%d: %s", len(replies), len(want), log)
	}
	for i, expected := range want {
		if !strings.Contains(replies[i], expected) {
			t.Fatalf("reply %d missing %q: %s", i, expected, replies[i])
		}
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
