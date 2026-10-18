package client

import (
	"github.com/kazuph/goterrace/internal/render"
	"os"
	"time"
)

func hasTraceImages(in render.Input) bool {
	for _, p := range in.Panes {
		if len(p.Graphics) > 0 {
			return true
		}
	}
	return false
}

func (m *machineClient) tracePaint(data []byte, probe bool, in render.Input) {
	if m.trace == nil {
		return
	}
	m.drawSeq++
	e := m.endpoints[m.active]
	m.trace.Event("terminal.written", " pid=", os.Getpid(), " endpoint=", m.active, " generation=", e.attempt, " draw_seq=", m.drawSeq, " boot_id=", e.boot, " revision=", in.SurfaceRevision, " bytes=", len(data), " images=", hasTraceImages(in), " text_only=", e.textOnly)
	if probe {
		m.traceProbeTimeout(time.Now())
		m.probeSeq = m.drawSeq
		m.probeUntil = m.outputFence.Deadline()
		m.probeEndpoint = m.active
		m.probeGeneration = e.attempt
		m.trace.Event("probe.sent", " pid=", os.Getpid(), " endpoint=", m.active, " generation=", e.attempt, " draw_seq=", m.probeSeq)
	}
}

func (m *machineClient) traceProbeTimeout(now time.Time) {
	if m.trace == nil {
		return
	}
	m.traceProbeContext()
	if m.probeSeq == 0 || now.Before(m.probeUntil) {
		return
	}
	seq := m.probeSeq
	m.probeSeq = 0
	m.probeUnanswered++
	m.trace.Event("probe.give_up", " pid=", os.Getpid(), " endpoint=", m.probeEndpoint, " generation=", m.probeGeneration, " draw_seq=", seq, " reason=timeout")
}

func (m *machineClient) traceProbeReply() {
	if m.trace == nil {
		return
	}
	// Accept already cleared the functional fence. Expiration is also processed
	// by advance; the trace keeps its own deadline for late-reply classification.
	m.traceProbeTimeout(time.Now())
	var seq uint64
	if m.probeDiscardNext {
		m.probeDiscardNext = false
	} else if m.probeUnanswered > 0 {
		m.probeUnanswered--
	} else {
		seq = m.probeSeq
		m.probeSeq = 0
	}
	m.trace.Event("probe.reply", " pid=", os.Getpid(), " endpoint=", m.probeEndpoint, " generation=", m.probeGeneration, " draw_seq=", seq, " matched=", seq != 0)
}

func (m *machineClient) traceProbeContext() {
	e := m.endpoints[m.active]
	if e == nil || m.probeEndpoint == m.active && m.probeGeneration == e.attempt {
		return
	}
	// A switch discards the old count but conservatively excludes one reply
	// when an old query could still answer on the same terminal input stream.
	m.probeDiscardNext = m.probeDiscardNext || m.probeSeq != 0 || m.probeUnanswered > 0
	m.probeUnanswered = 0
	m.probeSeq = 0
	m.probeEndpoint = m.active
	m.probeGeneration = e.attempt
}
