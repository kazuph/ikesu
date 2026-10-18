package server

import (
	"github.com/kazuph/goterrace/internal/render"
	"os"
	"time"
)

func (s *Server) traceSurface(c *clientConn, in render.Input, bytes int, textOnly bool) {
	images := false
	for _, p := range in.Panes {
		if len(p.Graphics) > 0 {
			images = true
			break
		}
	}
	s.trace.Event("surface.queued", " pid=", os.Getpid(), " client_id=", c.id, " boot_id=", s.decisions.boot, " revision=", in.SurfaceRevision, " bytes=", bytes, " images=", images, " text_only=", textOnly)
}

func (s *Server) traceReceiptEnd(c *clientConn, reason string) {
	if s.trace == nil || c.graphics.traceAwaiting == 0 {
		return
	}
	f := c.graphics
	revision, started := f.traceAwaiting, f.traceStarted
	f.traceAwaiting = 0
	f.traceStarted = time.Time{}
	s.trace.Event("receipt.ended", " client_id=", c.id, " reason=", reason, " revision=", revision, " waited_ms=", time.Since(started).Milliseconds())
}

// An ACK can be processed before the writer's success reaches the app loop.
// Remember only diagnostics until the actual successful publication is known.
func (s *Server) traceReceiptEarly(c *clientConn, reason string) {
	if s.trace == nil {
		return
	}
	f := c.graphics
	if f.writing && f.awaiting != 0 && f.traceAwaiting == 0 {
		f.traceEarlyRevision = f.awaiting
		f.traceEarlyReason = reason
	}
}

func (s *Server) traceReceiptDrained(c *clientConn, revision uint64, patch bool) {
	if !patch {
		return
	}
	f := c.graphics
	if f.traceAwaiting == revision {
		s.trace.Event("receipt.started", " client_id=", c.id, " revision=", revision)
	} else if f.traceEarlyRevision == revision {
		f.traceAwaiting = revision
		f.traceStarted = time.Now()
		s.trace.Event("receipt.started", " client_id=", c.id, " revision=", revision)
		s.traceReceiptEnd(c, f.traceEarlyReason)
	}
	f.traceEarlyRevision = 0
	f.traceEarlyReason = ""
}
