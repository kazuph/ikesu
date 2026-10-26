package server

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
)

// P4b bounds image receipt waits independently for every viewer.
const graphicsReceiptWait = 500 * time.Millisecond

type graphicsFlow struct {
	published          *graphicsPublishedSurface
	textSurface        *graphicsTextSurface
	encoder            endpoint.SnapshotEncoder
	revision, awaiting uint64
	pending            bool
	redraw             bool
	writing            bool
	until              time.Time
	traceAwaiting      uint64
	traceStarted       time.Time
	traceEarlyRevision uint64
	traceEarlyReason   string
}

func (s *Server) installGraphicsSnapshotSink(c *clientConn, peer endpoint.Peer) {
	flow := &graphicsFlow{}
	flow.encoder.LegacyGrids = !endpoint.HasFlatDiff(peer) || !endpoint.HasFlatDiff(s.endpointPeer)
	flow.encoder.GraphicsPatches = endpoint.HasGraphicsPatches(peer) && endpoint.HasGraphicsPatches(s.endpointPeer)
	c.graphics = flow
	c.viewer.SetSnapshotSink(func(in render.Input) error { return s.sendGraphicsSnapshot(c, in) })
}

func (s *Server) sendGraphicsSnapshot(c *clientConn, in render.Input) error {
	flow := c.graphics
	if flow.writing {
		flow.pending = true
		flow.redraw = true
		return nil
	}
	waiting := flow.awaiting != 0 && time.Now().Before(flow.until)
	textOnly := false
	relayout := false
	var candidate *graphicsTextSurface
	if waiting {
		candidate = captureGraphicsText(in)
		flow.pending = true
		textOnly = true
		relayout = !flow.textSurface.compatible(candidate)
		if relayout {
			if candidate == nil {
				candidate = captureGraphicsGeometry(in)
			}
			if !flow.textSurface.resized(candidate) {
				return nil
			}
			in = flow.textSurface.relayoutInput(in)
			in = s.nativeGraphicsInput(c, in)
		} else {
			in = flow.textSurface.input(in)
		}
	} else {
		flow.awaiting = 0
		s.traceReceiptEnd(c, "timeout")
		in = s.nativeGraphicsInput(c, in)
		candidate = captureGraphicsText(in)
		flow.pending = false
	}
	surface := graphicsSurface(in)
	revision := flow.revision + 1
	if flow.encoder.GraphicsPatches {
		in.SurfaceRevision = revision
	}
	var data []byte
	var err error
	if textOnly {
		data, err = flow.encoder.EncodeText(in)
	} else {
		data, err = flow.encoder.Encode(in)
	}
	if err != nil {
		_ = c.send(clientMsg{Type: "error", Reason: "endpoint_snapshot_invalid: " + err.Error()})
		s.dropConn(c)
		return err
	}
	message := clientMsg{Type: "snapshot", Data: encode64(data), RequestAck: c.viewer.AwaitingFocusRender(), FocusEpoch: c.viewer.FocusEpoch()}
	flow.writing = true
	// GraphicsPatches is added to the wire copy by the encoder.
	patchSent := !textOnly && flow.encoder.SentGraphicsPatch()
	if patchSent {
		flow.awaiting = revision
	}
	reply, err := c.queueGraphicsFrame(message)
	if err != nil {
		flow.writing = false
		return err
	}
	go func() {
		var result clientWriteResult
		select {
		case result = <-reply:
		case <-c.done:
			result.err = net.ErrClosed
		}
		s.app.Do(func() {
			flow.writing = false
			if result.err != nil {
				s.dropConn(c)
				return
			}
			flow.revision = revision
			if textOnly {
				flow.encoder.CommitText()
				if relayout {
					flow.encoder.ResetGraphics()
					flow.published = surface
				}
			} else {
				flow.published = surface
				flow.textSurface = candidate
				s.nativeGraphicsDrained(c, surface)
			}
			if flow.awaiting == revision {
				s.armGraphicsReceipt(c, revision)
			} else if flow.pending && (!textOnly || flow.awaiting == 0 || !time.Now().Before(flow.until)) {
				flow.pending = false
				c.viewer.Redraw(s.app)
			}
			if flow.redraw {
				flow.redraw = false
				c.viewer.Redraw(s.app)
			}
			if s.trace != nil {
				s.traceSurface(c, in, result.bytes, textOnly)
				s.traceReceiptDrained(c, revision, patchSent)
			}
		})
	}()
	return nil
}

func (s *Server) armGraphicsReceipt(c *clientConn, revision uint64) {
	flow := c.graphics
	flow.until = time.Now().Add(graphicsReceiptWait)
	if s.trace != nil {
		flow.traceAwaiting = revision
		flow.traceStarted = flow.until.Add(-graphicsReceiptWait)
	}
	time.AfterFunc(graphicsReceiptWait, func() {
		s.app.Do(func() {
			s.mu.Lock()
			live := s.clients[c]
			s.mu.Unlock()
			if live && flow.awaiting == revision {
				s.traceReceiptEnd(c, "timeout")
				if flow.pending {
					c.viewer.Redraw(s.app)
				}
			}
		})
	})
}

func (s *Server) releaseGraphicsReceipt(c *clientConn, reason string) {
	flow := c.graphics
	flow.awaiting = 0
	flow.until = time.Time{}
	s.traceReceiptEnd(c, reason)
	if flow.pending {
		flow.pending = false
		c.viewer.Redraw(s.app)
	}
}

func (s *Server) graphicsControl(c *clientConn, message clientMsg) {
	flow := c.graphics
	if flow == nil || !flow.encoder.GraphicsPatches {
		_ = c.send(clientMsg{Type: "error", Reason: "endpoint_action_unsupported: graphics patches"})
		return
	}
	switch message.Kind {
	case endpoint.GraphicsPatchAck:
		revision, err := strconv.ParseUint(message.Data, 10, 64)
		if err != nil {
			_ = c.send(clientMsg{Type: "error", Reason: "invalid graphics receipt revision"})
			return
		}
		if flow.awaiting != 0 && revision == flow.awaiting {
			s.traceReceiptEarly(c, "ack")
			s.releaseGraphicsReceipt(c, "ack")
		}
	case endpoint.GraphicsPatchMiss:
		// The observed contract intentionally ignores miss payload contents.
		s.traceReceiptEarly(c, "miss")
		flow.awaiting = 0
		flow.until = time.Time{}
		flow.pending = false
		flow.encoder.ResetGraphics()
		s.traceReceiptEnd(c, "miss")
		c.viewer.Redraw(s.app)
	default:
		_ = c.send(clientMsg{Type: "error", Reason: fmt.Sprintf("endpoint_action_unsupported: %s", message.Kind)})
	}
}
