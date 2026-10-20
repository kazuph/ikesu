package client

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/latency"
	"github.com/kazuph/ikesu/internal/render"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTraceBlockedRealIOAcceptsProbeAndClassifiesLateReply(t *testing.T) {
	old := latency.Enabled
	latency.Enabled = true
	defer func() { latency.Enabled = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	tr := latency.New(w)
	m := &machineClient{trace: tr, active: "saved", endpoints: map[string]*managedEndpoint{"saved": {attempt: 7, boot: "known-boot"}}}
	for i := 0; i < 100; i++ {
		tr.Event("blocked", strings.Repeat("x", 4096))
	}
	m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now())
	m.tracePaint([]byte("pixels"), true, render.Input{SurfaceRevision: 42})
	if err := m.input(input.Event{Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatal(err)
	}
	if m.probeSeq != 0 || !m.outputFence.Deadline().IsZero() {
		t.Fatal("logging prevented probe receipt")
	}
	m.outputFence.Written([]byte("\x1b_Ga=t,"), time.Now().Add(-2*render.TerminalStatusWait))
	m.tracePaint([]byte("pixels"), true, render.Input{SurfaceRevision: 43})
	if err := m.input(input.Event{Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatal(err)
	}
	if m.probeSeq != 0 || !m.outputFence.Deadline().IsZero() {
		t.Fatal("late reply left waiting")
	}
	drained := make(chan []byte)
	go func() { data, _ := io.ReadAll(r); drained <- data }()
	tr.Close()
	w.Close()
	data := <-drained
	for _, want := range []string{"event=probe.reply pid=", "draw_seq=1 matched=true", "draw_seq=2 reason=timeout", "draw_seq=0 matched=false", "generation=7", "boot_id=known-boot"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
}
